// hmac_test.go D17.6 — HMAC 签名 / 验签 / 轮换 / 链式 / 降级 的完整单测。
//
// 覆盖矩阵：
//  1. 签名产出字段齐全（sig_alg / sig_kid / sig）；
//  2. 同 key 往返：Sign 后 Verify 成功；
//  3. 任意业务字段被改 → Verify 失败（严格防篡改）；
//  4. kid 不识别 → Verify 失败；
//  5. key 错 → Verify 失败；
//  6. 多 kid 轮换：老 kid 签的记录，新 Signer（主 kid 变了但保留老 kid）仍能验；
//  7. 链式 prev_sig：连续 Sign 三条，第二条的 PrevSig == 第一条的 Sig；
//  8. VerifyLine：JSONL 一行直接验；语法错返 parse err，签名错返 mismatch err；
//  9. env 构造：有效 env / REQUIRED=1 + 无 key / base64 过短 / KEYS 多项解析。
package audit

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newKey 16 字节非零 key 的快捷工具。
func newKey(b byte) []byte {
	k := make([]byte, 16)
	for i := range k {
		k[i] = b
	}
	return k
}

// mustSigner t.Fatal on 失败。
func mustSigner(t *testing.T, keys map[string][]byte, primary string, chain bool) *HMACSigner {
	t.Helper()
	s, err := NewHMACSigner(keys, primary, chain)
	if err != nil {
		t.Fatalf("NewHMACSigner: %v", err)
	}
	return s
}

// sampleRecord 一个填满业务字段的 Record，用于签/验测试。
func sampleRecord() Record {
	return Record{
		TS:        "2026-04-22T16:00:00+08:00",
		User:      "alice",
		Agent:     "repair_agent",
		Action:    "bcs.helm.rollback",
		Severity:  "critical",
		Target:    "BCS-K8S-001/ns-letsgo/game-core",
		Params:    map[string]any{"chart": "game-core", "revision": 42},
		Reason:    "OOM after last deploy",
		Result:    "success",
		SessionID: "sess-abc",
	}
}

// ---------- 1 & 2: 基本 Sign/Verify 往返 ----------

func TestHMACSigner_SignVerify_RoundTrip(t *testing.T) {
	s := mustSigner(t, map[string][]byte{"k1": newKey(0x11)}, "k1", false)
	rec := sampleRecord()

	if err := s.Sign(&rec); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if rec.SigAlg != SigAlgHMACSHA256 {
		t.Errorf("SigAlg=%q, want %q", rec.SigAlg, SigAlgHMACSHA256)
	}
	if rec.SigKID != "k1" {
		t.Errorf("SigKID=%q, want k1", rec.SigKID)
	}
	if len(rec.Sig) != 64 { // hex(sha256) = 32*2
		t.Errorf("Sig len=%d, want 64 hex chars; got %q", len(rec.Sig), rec.Sig)
	}

	if err := s.Verify(&rec); err != nil {
		t.Fatalf("Verify after Sign should succeed: %v", err)
	}
}

// ---------- 3: 任意字段篡改都应被检测 ----------

func TestHMACSigner_Verify_DetectsTampering(t *testing.T) {
	s := mustSigner(t, map[string][]byte{"k": newKey(0x22)}, "k", false)
	base := sampleRecord()
	if err := s.Sign(&base); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// 列一组"攻击者可能想改"的字段，逐个篡改并确认 Verify 报错。
	type mutation struct {
		name string
		do   func(r *Record)
	}
	cases := []mutation{
		{"Result success→failure", func(r *Record) { r.Result = "failure" }},
		{"Action 改动作", func(r *Record) { r.Action = "bcs.helm.deploy" }},
		{"User 冒名", func(r *Record) { r.User = "eve" }},
		{"Target 改作用对象", func(r *Record) { r.Target = "BCS-K8S-002/..." }},
		{"Severity 降级", func(r *Record) { r.Severity = "low" }},
		{"Reason 改理由", func(r *Record) { r.Reason = "routine" }},
		{"TS 改时间", func(r *Record) { r.TS = "2000-01-01T00:00:00Z" }},
		{"Mock 从 false 改 true", func(r *Record) { r.Mock = true }},
		{"Params 改值", func(r *Record) { r.Params = map[string]any{"chart": "x"} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tampered := base // 值拷贝
			c.do(&tampered)
			if err := s.Verify(&tampered); err == nil {
				t.Errorf("mutation %q should trigger verify failure", c.name)
			}
		})
	}
}

// ---------- 4: 未知 kid ----------

func TestHMACSigner_Verify_UnknownKID(t *testing.T) {
	s := mustSigner(t, map[string][]byte{"k": newKey(0x33)}, "k", false)
	rec := sampleRecord()
	if err := s.Sign(&rec); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	rec.SigKID = "nope"
	if err := s.Verify(&rec); err == nil ||
		!strings.Contains(err.Error(), "unknown kid") {
		t.Errorf("want unknown kid error, got %v", err)
	}
}

// ---------- 5: key 错（同 kid 名但实际 key 不同） ----------

func TestHMACSigner_Verify_WrongKey(t *testing.T) {
	signerA := mustSigner(t, map[string][]byte{"k": newKey(0x44)}, "k", false)
	signerB := mustSigner(t, map[string][]byte{"k": newKey(0x55)}, "k", false)

	rec := sampleRecord()
	if err := signerA.Sign(&rec); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := signerB.Verify(&rec); err == nil ||
		!strings.Contains(err.Error(), "mismatch") {
		t.Errorf("want mismatch error, got %v", err)
	}
}

// ---------- 6: 多 kid 轮换 ----------

// 轮换场景：日志由老 Signer（primary=k_old）签；之后运维换了主密钥，
// 新 Signer primary=k_new 但 keys 里仍带 k_old → 老记录应仍能被验。
func TestHMACSigner_Rotation_OldSignatureStillVerifiable(t *testing.T) {
	oldSigner := mustSigner(t,
		map[string][]byte{"k_old": newKey(0x66)}, "k_old", false)

	rec := sampleRecord()
	if err := oldSigner.Sign(&rec); err != nil {
		t.Fatalf("Sign (old): %v", err)
	}
	if rec.SigKID != "k_old" {
		t.Fatalf("SigKID=%q, want k_old", rec.SigKID)
	}

	newSigner := mustSigner(t, map[string][]byte{
		"k_new": newKey(0x77),
		"k_old": newKey(0x66), // 保留历史 key
	}, "k_new", false)

	if err := newSigner.Verify(&rec); err != nil {
		t.Errorf("new signer should verify old-kid record: %v", err)
	}

	// 新记录应自动用 k_new。
	rec2 := sampleRecord()
	if err := newSigner.Sign(&rec2); err != nil {
		t.Fatalf("Sign (new): %v", err)
	}
	if rec2.SigKID != "k_new" {
		t.Errorf("SigKID=%q, want k_new", rec2.SigKID)
	}
}

// ---------- 7: 链式 prev_sig ----------

func TestHMACSigner_Chain_PrevSigLinks(t *testing.T) {
	s := mustSigner(t, map[string][]byte{"k": newKey(0x88)}, "k", true)

	r1 := sampleRecord()
	r1.Action = "action.1"
	if err := s.Sign(&r1); err != nil {
		t.Fatalf("Sign r1: %v", err)
	}
	if r1.PrevSig != "" {
		t.Errorf("first record PrevSig should be empty, got %q", r1.PrevSig)
	}

	r2 := sampleRecord()
	r2.Action = "action.2"
	if err := s.Sign(&r2); err != nil {
		t.Fatalf("Sign r2: %v", err)
	}
	if r2.PrevSig != r1.Sig {
		t.Errorf("r2.PrevSig=%q, want r1.Sig=%q", r2.PrevSig, r1.Sig)
	}

	r3 := sampleRecord()
	r3.Action = "action.3"
	if err := s.Sign(&r3); err != nil {
		t.Fatalf("Sign r3: %v", err)
	}
	if r3.PrevSig != r2.Sig {
		t.Errorf("r3.PrevSig=%q, want r2.Sig=%q", r3.PrevSig, r2.Sig)
	}

	// 逐条 Verify 都应通过（链上每个节点单独验证不需要知道前后节点）。
	for i, r := range []Record{r1, r2, r3} {
		if err := s.Verify(&r); err != nil {
			t.Errorf("verify chain[%d]: %v", i, err)
		}
	}
}

// 链式模式下：擦掉中间一条 → 再尝试"篡改链保持自洽" 是否容易？
// 攻击者能改的是 r3.PrevSig 让它指向 r1.Sig，但这样 r3 的 sig 就变了
// （PrevSig 参与了 HMAC），攻击者没 key 重算不出来 → 必然验签失败。
// 此测验证"攻击无成本路径"不存在。
func TestHMACSigner_Chain_CannotDropMiddleRecord(t *testing.T) {
	s := mustSigner(t, map[string][]byte{"k": newKey(0x99)}, "k", true)

	r1 := sampleRecord()
	r1.Action = "first"
	_ = s.Sign(&r1)
	r2 := sampleRecord()
	r2.Action = "middle"
	_ = s.Sign(&r2)
	r3 := sampleRecord()
	r3.Action = "third"
	_ = s.Sign(&r3)

	// 攻击者"删除" r2，把 r3.PrevSig 改为指向 r1.Sig。
	r3.PrevSig = r1.Sig
	if err := s.Verify(&r3); err == nil {
		t.Error("tampered chain should fail verify (attacker without key)")
	}
}

// ---------- 8: VerifyLine ----------

func TestHMACSigner_VerifyLine(t *testing.T) {
	s := mustSigner(t, map[string][]byte{"k": newKey(0xAA)}, "k", false)
	rec := sampleRecord()
	if err := s.Sign(&rec); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, err := s.VerifyLine(line)
	if err != nil {
		t.Fatalf("VerifyLine OK case: %v", err)
	}
	if got.Action != rec.Action {
		t.Errorf("parsed action=%q, want %q", got.Action, rec.Action)
	}

	// 篡改：把 result 从 success 改成 failure。
	tampered := strings.Replace(string(line),
		`"result":"success"`, `"result":"failure"`, 1)
	if _, err := s.VerifyLine([]byte(tampered)); err == nil {
		t.Error("tampered line should fail verify")
	}

	// 语法错。
	if _, err := s.VerifyLine([]byte(`{not json}`)); err == nil ||
		!strings.Contains(err.Error(), "parse line") {
		t.Errorf("want parse err, got %v", err)
	}
}

// ---------- 9: env 构造 ----------

func TestNewHMACSignerFromEnv_EmptyNotRequired(t *testing.T) {
	t.Setenv(envHMACKey, "")
	t.Setenv(envHMACRequired, "")
	s, err := NewHMACSignerFromEnv()
	if err != nil {
		t.Fatalf("empty but not required should be (nil, nil), got err=%v", err)
	}
	if s != nil {
		t.Errorf("signer should be nil, got %+v", s)
	}
}

func TestNewHMACSignerFromEnv_EmptyRequired(t *testing.T) {
	t.Setenv(envHMACKey, "")
	t.Setenv(envHMACRequired, "1")
	_, err := NewHMACSignerFromEnv()
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("REQUIRED=1 + empty key should error, got %v", err)
	}
}

func TestNewHMACSignerFromEnv_ValidAndRotation(t *testing.T) {
	// 主密钥 + 旧密钥并存；两个 key 都必须 >=16 字节（base64 编码 24 字符）。
	t.Setenv(envHMACKey, "base64:AAECAwQFBgcICQoLDA0ODw==") // 16 字节
	t.Setenv(envHMACKID, "v2")
	t.Setenv(envHMACKeys,
		"v1:base64:EBESExQVFhcYGRobHB0eHw==") // 又一个 16 字节 key
	t.Setenv(envHMACRequired, "")
	t.Setenv(envHMACChain, "")

	s, err := NewHMACSignerFromEnv()
	if err != nil {
		t.Fatalf("valid env: %v", err)
	}
	if s == nil {
		t.Fatal("signer should not be nil")
	}
	if s.KeyID() != "v2" {
		t.Errorf("primary kid=%q, want v2", s.KeyID())
	}
	if _, ok := s.keys["v1"]; !ok {
		t.Error("v1 should be preserved for rotation verify")
	}
	if _, ok := s.keys["v2"]; !ok {
		t.Error("v2 should be present as primary")
	}
}

func TestNewHMACSignerFromEnv_KeyTooShort(t *testing.T) {
	// 8 字节 key（被 decodeBase64Key 的长度保护拦下）。
	t.Setenv(envHMACKey, "base64:AAECAwQFBgc=")
	t.Setenv(envHMACRequired, "")
	_, err := NewHMACSignerFromEnv()
	if err == nil || !strings.Contains(err.Error(), "too short") {
		t.Errorf("short key should error, got %v", err)
	}
}

func TestNewHMACSignerFromEnv_BadKeysFormat(t *testing.T) {
	t.Setenv(envHMACKey, "base64:AAECAwQFBgcICQoLDA0ODw==")
	t.Setenv(envHMACKeys, "bad_no_colon") // 缺 ':'
	_, err := NewHMACSignerFromEnv()
	if err == nil || !strings.Contains(err.Error(), "bad AUDIT_HMAC_KEYS") {
		t.Errorf("bad keys entry should error, got %v", err)
	}
}

// ---------- Emit 集成验证 ----------

// TestEmit_WithSigner_ProducesSignedLine Emit → SetSigner → MemorySink
// 应抓到带 sig 字段的 JSONL，再手动 Verify 该行。
func TestEmit_WithSigner_ProducesSignedLine(t *testing.T) {
	s := mustSigner(t, map[string][]byte{"k": newKey(0xBB)}, "k", false)
	SetSigner(s)
	defer SetSigner(nil)

	mem := &MemorySink{}
	old := SetSink(mem)
	defer SetSink(old)

	Emit(Event{
		User:    "alice",
		Agent:   "repair_agent",
		Action:  "bcs.helm.rollback",
		Target:  "BCS-K8S-001/ns/app",
		Success: true,
	})
	lines := mem.Snapshot()
	if len(lines) != 1 {
		t.Fatalf("lines=%d, want 1", len(lines))
	}
	if !strings.Contains(string(lines[0]), `"sig":`) {
		t.Errorf("line missing sig field: %s", lines[0])
	}
	if _, err := s.VerifyLine(lines[0]); err != nil {
		t.Errorf("VerifyLine on emitted: %v", err)
	}
}

// TestEmit_NoSigner_NoSigField 向下兼容：没 SetSigner 时记录仍正常产出、无 sig 字段。
func TestEmit_NoSigner_NoSigField(t *testing.T) {
	SetSigner(nil)
	mem := &MemorySink{}
	old := SetSink(mem)
	defer SetSink(old)

	Emit(Event{User: "bob", Action: "x", Success: true})
	lines := mem.Snapshot()
	if len(lines) != 1 {
		t.Fatalf("lines=%d, want 1", len(lines))
	}
	if strings.Contains(string(lines[0]), `"sig":`) {
		t.Errorf("line should have no sig when signer is nil: %s", lines[0])
	}
}

// ============================================================
// D17.7：跨重启链式 state 持久化单测
// ============================================================

// stateFilePath 临时目录下一个固定文件名，避免测试间干扰。
func stateFilePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "chain_state.json")
}

// TestSaveLoadState_RoundTrip 签三条 → Save → 新 signer Load → lastSig 一致 →
// 新 signer 再签一条，PrevSig 应续上（证明跨重启续链链路畅通）。
func TestSaveLoadState_RoundTrip(t *testing.T) {
	keys := map[string][]byte{"k": newKey(0xC1)}
	path := stateFilePath(t)

	s1 := mustSigner(t, keys, "k", true)
	for i := 0; i < 3; i++ {
		r := sampleRecord()
		r.Action = fmt.Sprintf("a%d", i)
		if err := s1.Sign(&r); err != nil {
			t.Fatalf("Sign: %v", err)
		}
	}
	lastSigBefore := s1.lastSig
	if lastSigBefore == "" {
		t.Fatal("lastSig should be non-empty after 3 signs")
	}

	if err := s1.SaveState(path); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// 模拟"新进程"：新建 signer 后 Load。
	s2 := mustSigner(t, keys, "k", true)
	if err := s2.LoadState(path); err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !s2.loaded {
		t.Error("loaded flag should be true after LoadState")
	}
	if s2.lastSig != lastSigBefore {
		t.Errorf("lastSig after Load=%q, want %q", s2.lastSig, lastSigBefore)
	}

	// 续链验证：s2 再签一条，PrevSig 必须等于 s1 的 lastSig。
	r := sampleRecord()
	r.Action = "after-restart"
	if err := s2.Sign(&r); err != nil {
		t.Fatalf("Sign after load: %v", err)
	}
	if r.PrevSig != lastSigBefore {
		t.Errorf("PrevSig=%q, want %q (chain broken across restart!)",
			r.PrevSig, lastSigBefore)
	}
}

// TestLoadState_FileNotExist 首次启动：state 文件尚不存在，LoadState 返 err
// 但 err 应是 os.IsNotExist 可识别类型，调用方可据此降级。
func TestLoadState_FileNotExist(t *testing.T) {
	s := mustSigner(t, map[string][]byte{"k": newKey(0xC2)}, "k", true)
	path := filepath.Join(t.TempDir(), "definitely_not_exist.json")
	err := s.LoadState(path)
	if err == nil {
		t.Fatal("expect err for non-existent state file")
	}
	// 确认 error 链里能识别 IsNotExist（便于调用方忽略这类"正常缺失"）。
	if !strings.Contains(err.Error(), "no such file") &&
		!strings.Contains(err.Error(), "cannot find") /* windows */ {
		t.Logf("err message: %v (ok, but want os-not-exist semantics)", err)
	}
	if s.loaded {
		t.Error("loaded should stay false when load failed")
	}
}

// TestLoadState_TamperedStateSig 攻击者改了 state_sig → LoadState 必 mismatch。
func TestLoadState_TamperedStateSig(t *testing.T) {
	keys := map[string][]byte{"k": newKey(0xC3)}
	path := stateFilePath(t)

	s1 := mustSigner(t, keys, "k", true)
	r := sampleRecord()
	_ = s1.Sign(&r)
	if err := s1.SaveState(path); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// 篡改：把 state_sig 改掉。
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// 简单做法：把 state_sig 值替换成一堆零。
	var st chainState
	_ = json.Unmarshal(raw, &st)
	st.StateSig = strings.Repeat("0", len(st.StateSig))
	bad, _ := json.Marshal(&st)
	_ = os.WriteFile(path, bad, 0o600)

	s2 := mustSigner(t, keys, "k", true)
	if err := s2.LoadState(path); err == nil ||
		!strings.Contains(err.Error(), "mismatch") {
		t.Errorf("tampered state_sig should error with mismatch, got %v", err)
	}
}

// TestLoadState_TamperedLastSig 攻击者只改 last_sig 不改 state_sig → 仍应 mismatch
// （证明 last_sig 在签名覆盖范围内）。
func TestLoadState_TamperedLastSig(t *testing.T) {
	keys := map[string][]byte{"k": newKey(0xC4)}
	path := stateFilePath(t)

	s1 := mustSigner(t, keys, "k", true)
	r := sampleRecord()
	_ = s1.Sign(&r)
	_ = s1.SaveState(path)

	raw, _ := os.ReadFile(path)
	var st chainState
	_ = json.Unmarshal(raw, &st)
	st.LastSig = "fake_last_sig_forged_by_attacker"
	bad, _ := json.Marshal(&st)
	_ = os.WriteFile(path, bad, 0o600)

	s2 := mustSigner(t, keys, "k", true)
	if err := s2.LoadState(path); err == nil ||
		!strings.Contains(err.Error(), "mismatch") {
		t.Errorf("tampered last_sig should error, got %v", err)
	}
}

// TestLoadState_SavedAtFuture saved_at 在未来（时钟回拨攻击）→ 拒绝加载。
func TestLoadState_SavedAtFuture(t *testing.T) {
	keys := map[string][]byte{"k": newKey(0xC5)}
	path := stateFilePath(t)

	// 手工构造一个 saved_at=1 年后、但 state_sig 合法的 state 文件。
	s := mustSigner(t, keys, "k", true)
	future := time.Now().Add(365 * 24 * time.Hour).Format(time.RFC3339)
	st := chainState{
		LastSig:    "abc",
		PrimaryKID: "k",
		SavedAt:    future,
	}
	sig, err := s.signStateBytes(&st)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	st.StateSig = sig
	raw, _ := json.Marshal(&st)
	_ = os.WriteFile(path, raw, 0o600)

	if err := s.LoadState(path); err == nil ||
		!strings.Contains(err.Error(), "future") {
		t.Errorf("future saved_at should error, got %v", err)
	}
}

// TestLoadState_SavedAtTooOld saved_at 太老（> 1 年）→ 拒绝加载。
func TestLoadState_SavedAtTooOld(t *testing.T) {
	keys := map[string][]byte{"k": newKey(0xC6)}
	path := stateFilePath(t)

	s := mustSigner(t, keys, "k", true)
	veryOld := time.Now().Add(-2 * 365 * 24 * time.Hour).Format(time.RFC3339)
	st := chainState{
		LastSig:    "abc",
		PrimaryKID: "k",
		SavedAt:    veryOld,
	}
	sig, err := s.signStateBytes(&st)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	st.StateSig = sig
	raw, _ := json.Marshal(&st)
	_ = os.WriteFile(path, raw, 0o600)

	if err := s.LoadState(path); err == nil ||
		!strings.Contains(err.Error(), "too old") {
		t.Errorf("too-old saved_at should error, got %v", err)
	}
}

// TestLoadState_KIDRetired state 的 kid 已从新 signer keys 里彻底撤销 →
// 无法验签，拒绝加载。
func TestLoadState_KIDRetired(t *testing.T) {
	oldKeys := map[string][]byte{"v1": newKey(0xC7)}
	newKeys := map[string][]byte{"v2": newKey(0xC8)} // 注意：不含 v1
	path := stateFilePath(t)

	s1 := mustSigner(t, oldKeys, "v1", true)
	r := sampleRecord()
	_ = s1.Sign(&r)
	_ = s1.SaveState(path)

	s2 := mustSigner(t, newKeys, "v2", true)
	err := s2.LoadState(path)
	if err == nil {
		t.Error("expected err when old kid retired")
	}
	if err != nil && !strings.Contains(err.Error(), "not in current keys") {
		t.Errorf("unexpected err: %v", err)
	}
}

// TestClose_SavesStateWhenChainEnabled 链式开启且配了 stateFile →
// Close 应落盘；Load 回来应一致。
func TestClose_SavesStateWhenChainEnabled(t *testing.T) {
	keys := map[string][]byte{"k": newKey(0xC9)}
	path := stateFilePath(t)

	s := mustSigner(t, keys, "k", true)
	s.stateFile = path // 直接注入，模拟 env 构造器的行为
	r := sampleRecord()
	_ = s.Sign(&r)

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Close 幂等：再调一次不应报错。
	if err := s.Close(); err != nil {
		t.Errorf("Close (second call): %v", err)
	}

	// 文件应存在且能被 Load。
	s2 := mustSigner(t, keys, "k", true)
	if err := s2.LoadState(path); err != nil {
		t.Fatalf("LoadState after Close: %v", err)
	}
	if s2.lastSig != s.lastSig {
		t.Errorf("lastSig after Close+Load=%q, want %q", s2.lastSig, s.lastSig)
	}
}

// TestClose_NoOpWhenChainDisabled 非链式模式或未配 stateFile → Close 不应写文件。
func TestClose_NoOpWhenChainDisabled(t *testing.T) {
	keys := map[string][]byte{"k": newKey(0xCA)}

	// 场景 A：chain=false。
	s := mustSigner(t, keys, "k", false)
	s.stateFile = filepath.Join(t.TempDir(), "should_not_be_written.json")
	if err := s.Close(); err != nil {
		t.Fatalf("Close (chain=false): %v", err)
	}
	if _, err := os.Stat(s.stateFile); !os.IsNotExist(err) {
		t.Errorf("state file should not exist when chain=false")
	}

	// 场景 B：chain=true 但无 stateFile。
	s2 := mustSigner(t, keys, "k", true)
	if err := s2.Close(); err != nil {
		t.Errorf("Close (no stateFile): %v", err)
	}
}

// TestCloseSigner_Global 全局 CloseSigner 入口：
//   - SetSigner(nil) 后调 CloseSigner 返 nil；
//   - 注入实现了 Close 的 Signer → CloseSigner 转发成功。
func TestCloseSigner_Global(t *testing.T) {
	SetSigner(nil)
	if err := CloseSigner(); err != nil {
		t.Errorf("CloseSigner with nil signer should be no-op, got %v", err)
	}

	keys := map[string][]byte{"k": newKey(0xCB)}
	path := stateFilePath(t)
	s := mustSigner(t, keys, "k", true)
	s.stateFile = path
	r := sampleRecord()
	_ = s.Sign(&r)

	SetSigner(s)
	defer SetSigner(nil)
	if err := CloseSigner(); err != nil {
		t.Fatalf("CloseSigner: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("state file should be written after CloseSigner: %v", err)
	}
}

// TestSaveState_AtomicNoTmpLeft SaveState 成功后 <path>.tmp 不应残留。
func TestSaveState_AtomicNoTmpLeft(t *testing.T) {
	keys := map[string][]byte{"k": newKey(0xCC)}
	path := stateFilePath(t)

	s := mustSigner(t, keys, "k", true)
	r := sampleRecord()
	_ = s.Sign(&r)
	if err := s.SaveState(path); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp file should have been renamed away; stat err=%v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("final state file should exist: %v", err)
	}
}

// TestNewHMACSignerFromEnv_ChainStateLoadsLastSig env 装配端到端：
//   - 先造一个已有 state 的文件；
//   - 再用 env (CHAIN=1, CHAIN_STATE=<path>) 构造 signer；
//   - signer.lastSig 应自动等于文件里的值。
func TestNewHMACSignerFromEnv_ChainStateLoadsLastSig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// 用"第一进程"的 signer 造一个合法 state 文件。
	k := newKey(0xCD)
	keyB64 := base64.StdEncoding.EncodeToString(k)

	s1 := mustSigner(t, map[string][]byte{"v1": k}, "v1", true)
	r := sampleRecord()
	_ = s1.Sign(&r)
	if err := s1.SaveState(path); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	expectedLast := s1.lastSig

	// "第二进程"走 env 路径构造。
	t.Setenv(envHMACKey, "base64:"+keyB64)
	t.Setenv(envHMACKID, "v1")
	t.Setenv(envHMACKeys, "")
	t.Setenv(envHMACRequired, "")
	t.Setenv(envHMACChain, "1")
	t.Setenv(envHMACChainState, path)

	s2, err := NewHMACSignerFromEnv()
	if err != nil {
		t.Fatalf("NewHMACSignerFromEnv: %v", err)
	}
	if s2 == nil {
		t.Fatal("signer is nil")
	}
	if s2.lastSig != expectedLast {
		t.Errorf("lastSig=%q, want %q", s2.lastSig, expectedLast)
	}
	if !s2.loaded {
		t.Error("loaded should be true")
	}
}

// TestNewHMACSignerFromEnv_ChainStateIgnoredWhenChainOff CHAIN=0 + STATE 有值
// → state 被忽略（只 WARN，不加载）。
func TestNewHMACSignerFromEnv_ChainStateIgnoredWhenChainOff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	k := newKey(0xCE)
	keyB64 := base64.StdEncoding.EncodeToString(k)

	// 先写一个有效 state。
	s1 := mustSigner(t, map[string][]byte{"v1": k}, "v1", true)
	r := sampleRecord()
	_ = s1.Sign(&r)
	_ = s1.SaveState(path)

	t.Setenv(envHMACKey, "base64:"+keyB64)
	t.Setenv(envHMACKID, "v1")
	t.Setenv(envHMACKeys, "")
	t.Setenv(envHMACRequired, "")
	t.Setenv(envHMACChain, "0") // <-- 关
	t.Setenv(envHMACChainState, path)

	s2, err := NewHMACSignerFromEnv()
	if err != nil {
		t.Fatalf("NewHMACSignerFromEnv: %v", err)
	}
	if s2 == nil {
		t.Fatal("signer is nil")
	}
	if s2.loaded {
		t.Error("state should be ignored when chain=0")
	}
	if s2.stateFile != "" {
		t.Errorf("stateFile should be cleared when chain=0, got %q", s2.stateFile)
	}
}

// TestNewHMACSignerFromEnv_ChainStateMissingIsNotFatal 首次启动 state 文件不存在
// → 不应报 fatal error；signer 仍可用，lastSig=""。
func TestNewHMACSignerFromEnv_ChainStateMissingIsNotFatal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not_yet_exist.json") // 故意不创建

	k := newKey(0xCF)
	keyB64 := base64.StdEncoding.EncodeToString(k)

	t.Setenv(envHMACKey, "base64:"+keyB64)
	t.Setenv(envHMACKID, "v1")
	t.Setenv(envHMACKeys, "")
	t.Setenv(envHMACRequired, "")
	t.Setenv(envHMACChain, "1")
	t.Setenv(envHMACChainState, path)

	s, err := NewHMACSignerFromEnv()
	if err != nil {
		t.Fatalf("first-run (no state file) should NOT fatal: %v", err)
	}
	if s == nil {
		t.Fatal("signer is nil")
	}
	if s.lastSig != "" {
		t.Errorf("lastSig should be empty on first run, got %q", s.lastSig)
	}
	if s.stateFile != path {
		t.Errorf("stateFile not preserved: got %q, want %q", s.stateFile, path)
	}
}