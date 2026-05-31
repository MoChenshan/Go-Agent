// hmac.go D17.6 / D17.7 — 审计日志 HMAC 签名与验签（含跨重启链式 prev_sig 持久化）。
//
// 核心职责
//
//  1. 从环境变量解析 HMAC 密钥（支持多 kid 并存的轮换场景）；
//  2. 为 Record 生成 canonical 字节流（稳定 JSON），算 HMAC-SHA256 摘要；
//  3. 验签：给定一行审计 JSONL，抽出 sig/kid，重算 HMAC 对比；
//  4. 链式签名（prev_sig）：可选的链式模式，防中间篡删。
//  5. **D17.7 新增**：链式状态（lastSig）跨重启持久化，让链式签名真正生产可用。
//
// 设计取舍（与 D17.6 / D17.7 日报对齐）
//
//  - 签名范围：Record 中除 sig/sig_alg/sig_kid 之外的**全部字段**。
//    任一业务字段被改（例如 result 从 failure 改 success）都会导致验签失败。
//  - 签名放回原记录的 sig 字段，只输出一行 JSONL，MultiSink/RemoteSink batch
//    零改动；代价是 marshal 两次（低 QPS 可接受）。
//  - 密钥编码：强制 base64 前缀（key 含任意字节时字符串方式会截断）。
//  - 默认降级：未配 AUDIT_HMAC_KEY 时 Emit 不签名（本地开发零配置）；
//    生产通过 AUDIT_HMAC_REQUIRED=1 显式升级为"未配 key = fatal"。
//  - 链式默认关闭：AUDIT_HMAC_CHAIN=1 启用。
//  - 链式 state 文件自身也带 HMAC 签名：防止攻击者直接改 state.json 的 last_sig
//    让新链 "合法地" 指向一个伪造的前缀。
//  - 原子写：写临时文件 → fsync → rename，防 OOM/SIGKILL 时读到半截文件。
//  - 加载失败永不 panic：state 文件丢失/损坏/验签失败 → 退化为"新链从空开始"，
//    仅 WARN 日志提示，不阻塞 agent 启动（避免小故障放大为服务中断）。
package audit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// -------------------- 常量 --------------------

const (
	// SigAlgHMACSHA256 目前唯一支持的算法。保留字段便于未来扩展 HMAC-SHA512 / Ed25519。
	SigAlgHMACSHA256 = "HMAC-SHA256"

	// envHMACKey 主密钥（签名时用）。格式：base64:<标准 base64 编码>
	// 未配置时 Emit 不签名（除非 AUDIT_HMAC_REQUIRED=1）。
	envHMACKey = "AUDIT_HMAC_KEY"
	// envHMACKID 主密钥对应的 kid；未配置则默认 "default"。
	envHMACKID = "AUDIT_HMAC_KID"
	// envHMACKeys 轮换场景下的旧密钥集合。格式：kid1:base64:xxx,kid2:base64:yyy
	// 验签时按 kid 匹配；主 kid 会被自动合并进来无需重复声明。
	envHMACKeys = "AUDIT_HMAC_KEYS"
	// envHMACRequired 生产保底开关：=1 时未配 key 直接 panic。
	envHMACRequired = "AUDIT_HMAC_REQUIRED"
	// envHMACChain 启用链式签名（prev_sig 引用上一条 sig）。
	envHMACChain = "AUDIT_HMAC_CHAIN"
	// envHMACChainState 链式 state 文件路径（D17.7）。
	// 未配 → 不持久化；配了但 chain=0 → 无意义，会被忽略（并 WARN 一次）；
	// 配了且 chain=1 → 启动 Load，app.Close 时 Save。
	envHMACChainState = "AUDIT_HMAC_CHAIN_STATE"

	// stateMaxAgeDefault state 文件允许的最大 "saved_at 距今" 时间跨度。
	// 超过此跨度认为是陈旧文件（可能是回滚攻击或遗留文件），拒绝加载新链从零开始。
	stateMaxAgeDefault = 365 * 24 * time.Hour // 1 年
	// stateFutureSkewTolerance 允许 saved_at 在未来的最大偏差（防时钟回拨攻击）。
	stateFutureSkewTolerance = 5 * time.Minute
)

// -------------------- Signer 抽象 --------------------

// Signer 给 Record 盖章的能力。
//
// 设计意图：未来要是 HMAC 不够用（比如换 KMS / 异步批量签），
// 只需另写一个 Signer 实现，不侵入 Emit 主路径。
type Signer interface {
	// Sign 就地修改 rec，填入 SigAlg / SigKID / PrevSig / Sig。
	// 返回 err 表示"签名失败但调用方可以选择继续落盘不签名的记录"；
	// 现阶段 HMAC 实现永远不返错，此 err 保留给未来 KMS 异步场景。
	Sign(rec *Record) error
	// KeyID 返回当前用于签名的主 kid（便于日志打印）。
	KeyID() string
}

// -------------------- HMAC 实现 --------------------

// HMACSigner env-driven HMAC 签名器。goroutine 安全。
type HMACSigner struct {
	// primaryKID 本进程签名所用 kid。
	primaryKID string
	// keys 所有已知 kid → 原始字节 key 的映射（含 primary）。
	// 用于本进程内"边签边验"场景（测试或链式校验 prev_sig）。
	keys map[string][]byte

	// chain 链式签名开关。
	chain bool
	// chainMu 保护 lastSig（链式模式下读写同一字段）。
	chainMu sync.Mutex
	// lastSig 本进程最近一次 Sign 成功后产生的 sig 值。
	// 链式模式下作为下一条的 prev_sig；首条为 ""。
	lastSig string

	// D17.7：链式 state 跨重启持久化。

	// stateFile state 文件绝对路径。"" 表示不持久化。
	stateFile string
	// loaded 标记本进程是否已经从 state 文件加载过 lastSig（仅用于日志/测试）。
	loaded bool
}

// NewHMACSignerFromEnv 按 env 构造；**未配主密钥**时：
//   - AUDIT_HMAC_REQUIRED=1 → 返回 error（让 main fail-fast）；
//   - 否则 → 返回 (nil, nil)，调用方 Emit 走"不签名"路径。
//
// 这是刻意的三态设计：让"本地开发零配置"和"生产强依赖"用同一套代码。
func NewHMACSignerFromEnv() (*HMACSigner, error) {
	primary := strings.TrimSpace(os.Getenv(envHMACKey))
	required := envTruthy(envHMACRequired)
	if primary == "" {
		if required {
			return nil, fmt.Errorf("audit HMAC: %s is required but empty", envHMACKey)
		}
		return nil, nil
	}

	primaryRaw, err := decodeBase64Key(primary)
	if err != nil {
		return nil, fmt.Errorf("audit HMAC: parse %s: %w", envHMACKey, err)
	}
	primaryKID := strings.TrimSpace(os.Getenv(envHMACKID))
	if primaryKID == "" {
		primaryKID = "default"
	}

	keys := map[string][]byte{primaryKID: primaryRaw}
	// 合并旧密钥集合（格式：kid:base64:xxx,kid2:base64:yyy）。
	if extra := strings.TrimSpace(os.Getenv(envHMACKeys)); extra != "" {
		for _, pair := range strings.Split(extra, ",") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			// 期待格式 kid:base64:<raw>，所以要按首个 ':' 切。
			idx := strings.Index(pair, ":")
			if idx <= 0 || idx == len(pair)-1 {
				return nil, fmt.Errorf("audit HMAC: bad %s entry %q "+
					"(want kid:base64:xxx)", envHMACKeys, pair)
			}
			kid := strings.TrimSpace(pair[:idx])
			val := strings.TrimSpace(pair[idx+1:])
			raw, err := decodeBase64Key(val)
			if err != nil {
				return nil, fmt.Errorf("audit HMAC: parse %s[%s]: %w",
					envHMACKeys, kid, err)
			}
			if _, dup := keys[kid]; dup {
				// 同 kid 重复声明（主密钥又出现在 KEYS 里）—— 以主为准，不覆盖。
				continue
			}
			keys[kid] = raw
		}
	}

	s := &HMACSigner{
		primaryKID: primaryKID,
		keys:       keys,
		chain:      envTruthy(envHMACChain),
		stateFile:  strings.TrimSpace(os.Getenv(envHMACChainState)),
	}

	// D17.7：链式 state 加载。
	//   - 未开 chain：忽略 stateFile（即便配了也无意义，只 WARN 一次）；
	//   - 开了 chain 但未配 stateFile：允许，行为与 D17.6 一致（仅单进程内续链）；
	//   - 都配了：尝试 Load；失败不 panic，只 WARN + lastSig="" 从零开始。
	if s.stateFile != "" {
		if !s.chain {
			log.Printf("[audit HMAC] %s set but %s=0, state file will be ignored",
				envHMACChainState, envHMACChain)
			s.stateFile = ""
		} else if err := s.LoadState(s.stateFile); err != nil {
			log.Printf("[audit HMAC] load chain state %s failed: %v "+
				"(new chain will start from empty prev_sig)", s.stateFile, err)
		}
	}
	return s, nil
}

// NewHMACSigner 程序内直接构造（测试 & 库级调用）。
//   - keys：kid → raw key 字节，必须包含 primaryKID。
//   - primaryKID：用于签名的主 kid。
//   - chain：是否启用链式 prev_sig。
func NewHMACSigner(keys map[string][]byte, primaryKID string, chain bool) (*HMACSigner, error) {
	if len(keys) == 0 {
		return nil, errors.New("NewHMACSigner: keys is empty")
	}
	if _, ok := keys[primaryKID]; !ok {
		return nil, fmt.Errorf("NewHMACSigner: primaryKID %q not in keys", primaryKID)
	}
	// 深拷贝一份，避免外部修改 map 影响签名器状态。
	cloned := make(map[string][]byte, len(keys))
	for k, v := range keys {
		cp := make([]byte, len(v))
		copy(cp, v)
		cloned[k] = cp
	}
	return &HMACSigner{
		primaryKID: primaryKID,
		keys:       cloned,
		chain:      chain,
	}, nil
}

// KeyID 实现 Signer.KeyID。
func (s *HMACSigner) KeyID() string { return s.primaryKID }

// Sign 实现 Signer.Sign。就地修改 rec 的签名字段。
//
// 流程：
//  1. 清空 rec 上已有的 sig 相关字段（避免"重复签名" bug）；
//  2. 链式模式下塞入 PrevSig；
//  3. Marshal 出 canonical 字节（Record 的 sig 字段用 omitempty 兜底，marshal 时为空自动忽略）；
//  4. HMAC-SHA256；
//  5. 填 SigAlg/SigKID/Sig 回 rec；
//  6. 链式模式下更新 lastSig。
func (s *HMACSigner) Sign(rec *Record) error {
	if s == nil || rec == nil {
		return nil
	}
	key, ok := s.keys[s.primaryKID]
	if !ok {
		return fmt.Errorf("audit HMAC: primary kid %q missing", s.primaryKID)
	}

	// 1 + 2: 复位旧 sig，填 prev_sig。
	rec.Sig = ""
	rec.SigAlg = SigAlgHMACSHA256
	rec.SigKID = s.primaryKID
	if s.chain {
		s.chainMu.Lock()
		rec.PrevSig = s.lastSig
		s.chainMu.Unlock()
	} else {
		rec.PrevSig = ""
	}

	// 3: canonical marshal（Go stdlib json 对 struct 字段按声明序稳定输出）。
	payload, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("audit HMAC: marshal canonical: %w", err)
	}

	// 4: HMAC。
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	sig := hex.EncodeToString(mac.Sum(nil))

	// 5: 写回。
	rec.Sig = sig

	// 6: 链式推进。
	if s.chain {
		s.chainMu.Lock()
		s.lastSig = sig
		s.chainMu.Unlock()
	}
	return nil
}

// Verify 独立的单条验签。
//
// 步骤：
//  1. 从 rec 上拷贝出 sig，随后清空 rec.Sig；
//  2. 按同样规则 canonical marshal；
//  3. HMAC-SHA256 重算并 constant-time 比较；
//  4. 失败返 error；成功返 nil（不恢复 rec.Sig，调用方若关心请自行备份）。
//
// 设计注意：Verify **不做链式校验**（prev_sig 已在 Sign 时当作字段参与了 HMAC，
// 所以自然覆盖在单条 HMAC 里；真正的"链完整性"需要离线遍历所有记录按顺序对照
// prev_sig 与前一条的 sig）。离线 CLI 负责那一层，见 cmd/auditverify/main.go。
func (s *HMACSigner) Verify(rec *Record) error {
	if s == nil {
		return errors.New("audit HMAC: signer is nil")
	}
	if rec == nil {
		return errors.New("audit HMAC: record is nil")
	}
	if rec.SigAlg != "" && rec.SigAlg != SigAlgHMACSHA256 {
		return fmt.Errorf("audit HMAC: unsupported alg %q", rec.SigAlg)
	}
	expected := rec.Sig
	if expected == "" {
		return errors.New("audit HMAC: record has no sig")
	}
	kid := rec.SigKID
	if kid == "" {
		kid = s.primaryKID
	}
	key, ok := s.keys[kid]
	if !ok {
		return fmt.Errorf("audit HMAC: unknown kid %q", kid)
	}

	// 清空 sig 后再 marshal —— 与 Sign 的步骤 3 严格对齐。
	rec.Sig = ""
	payload, err := json.Marshal(rec)
	// 恢复原样，避免副作用（Verify 应该是只读语义）。
	rec.Sig = expected
	if err != nil {
		return fmt.Errorf("audit HMAC: marshal canonical: %w", err)
	}

	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	actual := hex.EncodeToString(mac.Sum(nil))

	// constant-time 比较防时序攻击。
	if !hmac.Equal([]byte(expected), []byte(actual)) {
		return fmt.Errorf("audit HMAC: signature mismatch (kid=%s)", kid)
	}
	return nil
}

// VerifyLine 便捷方法：给一行 JSONL 直接验签。离线 CLI 的主要入口。
func (s *HMACSigner) VerifyLine(line []byte) (*Record, error) {
	var rec Record
	if err := json.Unmarshal(line, &rec); err != nil {
		return nil, fmt.Errorf("audit HMAC: parse line: %w", err)
	}
	if err := s.Verify(&rec); err != nil {
		return &rec, err
	}
	return &rec, nil
}

// -------------------- 全局 Signer 钩子 --------------------

var (
	signerMu sync.RWMutex
	signer   Signer
)

// SetSigner 注入全局 Signer（通常在 app.Init 里调用）。
// 传 nil 表示关闭签名（回到未签名兼容模式）。
func SetSigner(s Signer) {
	signerMu.Lock()
	defer signerMu.Unlock()
	signer = s
}

// activeSigner 拿当前 Signer。
func activeSigner() Signer {
	signerMu.RLock()
	defer signerMu.RUnlock()
	return signer
}

// -------------------- D17.7 链式 state 持久化 --------------------

// chainState 持久化到磁盘的链式状态。
//
// 格式说明：
//   - LastSig / PrimaryKID / SavedAt 构成 canonical payload（按声明序 marshal）；
//   - StateSig = HMAC-SHA256(key[PrimaryKID], canonical payload)；
//   - 加载时清空 StateSig 再 marshal 验签，逻辑与 Record 签验完全同构。
//
// 为什么 PrimaryKID 要入签：防止攻击者把 kid 改成自己控制的值混淆 Load 逻辑。
// 为什么 SavedAt 要入签：防"拿一个更早时间的 state 文件覆盖现在的"回滚攻击。
type chainState struct {
	LastSig    string `json:"last_sig"`
	PrimaryKID string `json:"primary_kid"`
	SavedAt    string `json:"saved_at"` // RFC3339
	StateSig   string `json:"state_sig,omitempty"`
}

// SaveState 把 lastSig 持久化到指定文件。
//
// 原子写协议：
//  1. Marshal + HMAC 签名；
//  2. 写到 <path>.tmp；
//  3. f.Sync() 让数据真正落盘；
//  4. os.Rename(<path>.tmp, <path>) 原子替换。
//
// 调用场景：
//   - app.Close() 保底路径（主要靠这条）；
//   - 将来若要加 ticker 周期写，可以多调几次，本方法幂等 & 线程安全。
func (s *HMACSigner) SaveState(path string) error {
	if s == nil {
		return errors.New("audit HMAC: signer is nil")
	}
	if path == "" {
		return errors.New("audit HMAC: state path is empty")
	}
	if !s.chain {
		// 非链式模式保存毫无意义（没 lastSig），但不算错 —— 幂等返回。
		return nil
	}

	s.chainMu.Lock()
	last := s.lastSig
	s.chainMu.Unlock()

	st := chainState{
		LastSig:    last,
		PrimaryKID: s.primaryKID,
		SavedAt:    time.Now().Format(time.RFC3339),
	}
	sig, err := s.signStateBytes(&st)
	if err != nil {
		return fmt.Errorf("sign state: %w", err)
	}
	st.StateSig = sig

	payload, err := json.Marshal(&st)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	// 原子写：tmp → fsync → rename。
	// 注意 tmp 放在同一目录（不能跨 fs，否则 rename 不原子）。
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	tmp := path + ".tmp"
	// 0600：state 文件含 sig 值，对其他用户不可读更安全。
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open tmp: %w", err)
	}
	if _, err := f.Write(payload); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write tmp: %w", err)
	}
	// 显式换行便于 cat 查看。
	if _, err := f.Write([]byte("\n")); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write nl: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("sync tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// LoadState 从指定文件加载 lastSig，严格验签并检查 saved_at 合理范围。
//
// 失败路径约定：
//   - 文件不存在 → 返回 err（调用方可判 os.IsNotExist 决定是否报）；
//     在 NewHMACSignerFromEnv 里"首次启动" 是正常场景，所以只 WARN 不阻塞；
//   - 签名错/格式错/时间越界 → 返回 err，lastSig 保持原样（通常是零值 "")。
//
// 成功路径：s.lastSig = state.LastSig，s.loaded = true。
func (s *HMACSigner) LoadState(path string) error {
	if s == nil {
		return errors.New("audit HMAC: signer is nil")
	}
	if path == "" {
		return errors.New("audit HMAC: state path is empty")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	var st chainState
	if err := json.Unmarshal(raw, &st); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	if st.StateSig == "" {
		return errors.New("state missing state_sig")
	}
	if st.PrimaryKID == "" {
		return errors.New("state missing primary_kid")
	}
	// state 的 kid 必须仍在 keys 里（否则无法验签）。
	if _, ok := s.keys[st.PrimaryKID]; !ok {
		return fmt.Errorf("state primary_kid %q not in current keys "+
			"(retire too aggressive?)", st.PrimaryKID)
	}

	// 验签：清空 StateSig 再 marshal，重算 HMAC 比较。
	expected := st.StateSig
	st.StateSig = ""
	actual, err := s.signStateBytes(&st)
	st.StateSig = expected // 恢复（无副作用语义）
	if err != nil {
		return fmt.Errorf("resign: %w", err)
	}
	if !hmac.Equal([]byte(expected), []byte(actual)) {
		return fmt.Errorf("state_sig mismatch (tampered?)")
	}

	// saved_at 时间范围校验：防回滚攻击（太老）+ 防时钟回拨（未来超范围）。
	savedAt, err := time.Parse(time.RFC3339, st.SavedAt)
	if err != nil {
		return fmt.Errorf("parse saved_at: %w", err)
	}
	now := time.Now()
	if savedAt.After(now.Add(stateFutureSkewTolerance)) {
		return fmt.Errorf("saved_at %s is in the future (clock skew?)",
			st.SavedAt)
	}
	if now.Sub(savedAt) > stateMaxAgeDefault {
		return fmt.Errorf("saved_at %s is too old (> %s)",
			st.SavedAt, stateMaxAgeDefault)
	}

	// 一切通过 → 注入 lastSig。
	s.chainMu.Lock()
	s.lastSig = st.LastSig
	s.chainMu.Unlock()
	s.loaded = true
	return nil
}

// Close 如果有 stateFile 配置则保存一次，否则空实现。幂等。
//
// 由 app.Close() 调用，是生产路径的主要"写"时机。
func (s *HMACSigner) Close() error {
	if s == nil {
		return nil
	}
	if s.stateFile == "" || !s.chain {
		return nil
	}
	return s.SaveState(s.stateFile)
}

// signStateBytes 为 chainState 计算 HMAC。输入 st.StateSig 必须是 ""。
//
// 算法与 Record 签名完全同构：
//   - Marshal st（StateSig omitempty 空 = 不入 payload）；
//   - HMAC-SHA256；
//   - hex 返回。
func (s *HMACSigner) signStateBytes(st *chainState) (string, error) {
	key, ok := s.keys[st.PrimaryKID]
	if !ok {
		return "", fmt.Errorf("primary kid %q not in keys", st.PrimaryKID)
	}
	payload, err := json.Marshal(st)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// CloseSigner 全局 Signer 的优雅关闭入口。
//
// 由 app.Close() 调用；如果全局 Signer 实现了 Close() 方法则调用之，
// 否则 no-op。设计目的：给未来的 KMS Signer / 异步批量 Signer 留扩展点，
// 让"app 关闭 → signer 关闭"这个调用链保持稳定。
func CloseSigner() error {
	signerMu.RLock()
	s := signer
	signerMu.RUnlock()
	if s == nil {
		return nil
	}
	if c, ok := s.(interface{ Close() error }); ok {
		return c.Close()
	}
	return nil
}

// -------------------- 工具函数 --------------------

// decodeBase64Key 解析 "base64:<std-base64-payload>" 或纯 base64。
//
// 为什么强制 base64：HMAC key 可含任意字节（包括 0x00），
// 用明文字符串会被截断 / 被 shell 转义破坏。base64 是最省心的约定。
func decodeBase64Key(v string) ([]byte, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, errors.New("empty key")
	}
	// 允许前缀 "base64:" 以显式标注编码方式；不带前缀也当 base64 处理。
	if strings.HasPrefix(v, "base64:") {
		v = strings.TrimPrefix(v, "base64:")
	}
	raw, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}
	if len(raw) < 16 {
		// 16 字节 = 128 bit，HMAC-SHA256 最低可接受熵；阻止空串 / 过短误配。
		return nil, fmt.Errorf("key too short (%d bytes, want >=16)", len(raw))
	}
	return raw, nil
}

// envTruthy 解析布尔类 env（1/true/yes/on 为真）。
func envTruthy(k string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(k))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
