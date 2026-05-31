package wecom

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const encodingAESKeyLength = 43

type encryptedReplyBody struct {
	Encrypt      string `json:"encrypt"`
	MsgSignature string `json:"msgsignature"`
	Timestamp    int64  `json:"timestamp"`
	Nonce        string `json:"nonce"`
}

// msgCrypt implements enterprise WeChat's message encryption/decryption.
// Reference: https://developer.work.weixin.qq.com/document/path/90968
type msgCrypt struct {
	token  string
	aesKey []byte
	corpID string
}

// newMsgCrypt creates a new message crypto handler.
// encodingAESKey must be 43 characters (base64-encoded 256-bit key).
func newMsgCrypt(token, encodingAESKey, corpID string) (*msgCrypt, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("wecom crypto: empty token")
	}
	if len(encodingAESKey) != encodingAESKeyLength {
		return nil, fmt.Errorf(
			"wecom crypto: encoding_aes_key must be 43 chars, "+
				"got %d",
			len(encodingAESKey),
		)
	}
	// For smart-bot (智能机器人) scenarios, corpID should be empty string ""
	// per official docs: https://developer.work.weixin.qq.com/document/path/101033
	// "企业自建智能机器人的使用场景里，receiveid直接传空字符串即可"

	aesKey, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil {
		return nil, fmt.Errorf("wecom crypto: decode aes key: %w", err)
	}

	return &msgCrypt{
		token:  token,
		aesKey: aesKey,
		corpID: corpID,
	}, nil
}

// VerifyURL verifies the callback URL during WeChat webhook registration.
// Returns the decrypted echostr.
func (mc *msgCrypt) VerifyURL(msgSignature, timestamp, nonce, echostr string) ([]byte, error) {
	if !mc.verifySignature(msgSignature, timestamp, nonce, echostr) {
		return nil, errors.New("wecom crypto: signature verification failed")
	}

	plaintext, err := mc.decrypt(echostr)
	if err != nil {
		return nil, fmt.Errorf("wecom crypto: decrypt echostr: %w", err)
	}

	return plaintext, nil
}

// DecryptMsg decrypts an incoming message from the POST body.
func (mc *msgCrypt) DecryptMsg(msgSignature, timestamp, nonce string, body []byte) ([]byte, error) {
	var encrypted EncryptedBody
	if err := json.Unmarshal(body, &encrypted); err != nil {
		return nil, fmt.Errorf("wecom crypto: unmarshal encrypted body: %w", err)
	}

	if !mc.verifySignature(msgSignature, timestamp, nonce, encrypted.Encrypt) {
		return nil, errors.New("wecom crypto: signature verification failed")
	}

	plaintext, err := mc.decrypt(encrypted.Encrypt)
	if err != nil {
		return nil, fmt.Errorf("wecom crypto: decrypt message: %w", err)
	}

	return plaintext, nil
}

// EncryptReply encrypts a passive reply payload for webhook callbacks.
func (mc *msgCrypt) EncryptReply(
	plaintext []byte,
	timestamp string,
	nonce string,
) ([]byte, error) {
	if mc == nil {
		return nil, errors.New("wecom crypto: nil msgCrypt")
	}

	replyTimestamp, replyUnix := normalizeReplyTimestamp(timestamp)
	encrypted, err := mc.encrypt(plaintext)
	if err != nil {
		return nil, fmt.Errorf("wecom crypto: encrypt reply: %w", err)
	}

	payload := encryptedReplyBody{
		Encrypt: encrypted,
		MsgSignature: buildCryptoSignature(
			mc.token,
			replyTimestamp,
			nonce,
			encrypted,
		),
		Timestamp: replyUnix,
		Nonce:     nonce,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf(
			"wecom crypto: marshal reply: %w",
			err,
		)
	}
	return data, nil
}

// verifySignature checks the SHA1 signature.
// signature = SHA1(sort(token, timestamp, nonce, encrypt))
func (mc *msgCrypt) verifySignature(signature, timestamp, nonce, encrypt string) bool {
	expected := buildCryptoSignature(
		mc.token,
		timestamp,
		nonce,
		encrypt,
	)

	match := strings.EqualFold(expected, signature)
	if !match {
		// Debug log for troubleshooting
		fmt.Printf("[wecom-crypto] verifySignature FAILED:\n")
		fmt.Printf("  token_len=%d, token_first3=%s\n", len(mc.token), safeSubstr(mc.token, 3))
		fmt.Printf("  timestamp=%s, nonce=%s\n", timestamp, nonce)
		fmt.Printf("  encrypt_len=%d\n", len(encrypt))
		fmt.Printf("  expected_sig=%s\n", expected)
		fmt.Printf("  received_sig=%s\n", signature)
	}
	return match
}

func buildCryptoSignature(
	token string,
	timestamp string,
	nonce string,
	encrypt string,
) string {
	strs := []string{token, timestamp, nonce, encrypt}
	sort.Strings(strs)

	h := sha1.New()
	h.Write([]byte(strings.Join(strs, "")))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func normalizeReplyTimestamp(raw string) (string, int64) {
	trimmed := strings.TrimSpace(raw)
	if trimmed != "" {
		value, err := strconv.ParseInt(trimmed, 10, 64)
		if err == nil {
			return trimmed, value
		}
	}

	now := time.Now().Unix()
	return strconv.FormatInt(now, 10), now
}

// safeSubstr returns first n chars or full string if shorter
func safeSubstr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// decrypt decrypts a base64-encoded AES-CBC encrypted string.
// Format after decryption: random(16) + msgLen(4, network byte order) + msg + corpID
func (mc *msgCrypt) decrypt(ciphertext string) ([]byte, error) {
	cipherBytes, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}

	fmt.Printf("[wecom-crypto] decrypt: ciphertext_len=%d, cipherBytes_len=%d, aesKey_len=%d\n",
		len(ciphertext), len(cipherBytes), len(mc.aesKey))

	block, err := aes.NewCipher(mc.aesKey)
	if err != nil {
		return nil, fmt.Errorf("new aes cipher: %w", err)
	}

	if len(cipherBytes) < aes.BlockSize {
		return nil, errors.New("ciphertext too short")
	}
	if len(cipherBytes)%aes.BlockSize != 0 {
		return nil, errors.New("ciphertext not multiple of block size")
	}

	// AES-CBC, IV = first 16 bytes of AES key.
	iv := mc.aesKey[:aes.BlockSize]
	mode := cipher.NewCBCDecrypter(block, iv)
	plainBytes := make([]byte, len(cipherBytes))
	mode.CryptBlocks(plainBytes, cipherBytes)

	fmt.Printf("[wecom-crypto] decrypt: plainBytes_len=%d, first16=%v, last16=%v\n",
		len(plainBytes), plainBytes[:min(16, len(plainBytes))], plainBytes[max(0, len(plainBytes)-16):])

	// Remove PKCS#7 padding.
	plainBytes, err = pkcs7Unpad(plainBytes)
	if err != nil {
		return nil, fmt.Errorf("pkcs7 unpad: %w", err)
	}

	// Parse: random(16) + msgLen(4) + msg + corpID
	if len(plainBytes) < 20 {
		return nil, errors.New("plaintext too short after unpad")
	}

	msgLenBytes := plainBytes[16:20]
	msgLen := binary.BigEndian.Uint32(msgLenBytes)

	if uint32(len(plainBytes)) < 20+msgLen {
		return nil, errors.New("invalid message length")
	}

	msg := plainBytes[20 : 20+msgLen]
	receivedCorpID := string(plainBytes[20+msgLen:])

	// Always validate receiveid (corpID).
	// For smart-bot scenarios corpID is "", and the decrypted tail is also "",
	// so this check passes naturally.
	if receivedCorpID != mc.corpID {
		return nil, fmt.Errorf("receiveid mismatch: expected %q, got %q", mc.corpID, receivedCorpID)
	}

	return msg, nil
}

// encrypt encrypts a plaintext message for sending.
func (mc *msgCrypt) encrypt(plaintext []byte) (string, error) {
	// Build: random(16) + msgLen(4) + msg + corpID
	randomBytes := make([]byte, 16)
	for i := range randomBytes {
		randomBytes[i] = byte(rand.Intn(256)) //nolint:gosec
	}

	msgLen := make([]byte, 4)
	binary.BigEndian.PutUint32(msgLen, uint32(len(plaintext)))

	buf := make([]byte, 0, 16+4+len(plaintext)+len(mc.corpID))
	buf = append(buf, randomBytes...)
	buf = append(buf, msgLen...)
	buf = append(buf, plaintext...)
	buf = append(buf, []byte(mc.corpID)...)

	// PKCS#7 pad.
	buf = pkcs7Pad(buf, aes.BlockSize)

	block, err := aes.NewCipher(mc.aesKey)
	if err != nil {
		return "", fmt.Errorf("new aes cipher: %w", err)
	}

	iv := mc.aesKey[:aes.BlockSize]
	cipherBytes := make([]byte, len(buf))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(cipherBytes, buf)

	return base64.StdEncoding.EncodeToString(cipherBytes), nil
}

// pkcs7Pad pads data to blockSize using PKCS#7.
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padBytes := make([]byte, padding)
	for i := range padBytes {
		padBytes[i] = byte(padding)
	}
	return append(data, padBytes...)
}

// aesKeySize is the AES-256 key size (32 bytes).
// WeChat uses AES-256-CBC, so the max padding value is 32, not 16.
const aesKeySize = 32

// DecryptFile decrypts an encrypted file from WeChat AI Bot.
// Reference: https://developer.work.weixin.qq.com/document/path/100719#文件
//
// WeChat AI Bot encrypts files using AES-256-CBC:
// - Key: EncodingAESKey (same as message encryption)
// - IV: First 16 bytes of AES Key
// - Padding: PKCS#7 padded to 32 bytes boundary
//
// Returns the decrypted file content.
func (mc *msgCrypt) DecryptFile(encryptedData []byte) ([]byte, error) {
	return DecryptFileWithKey(mc.aesKey, encryptedData)
}

// GetAESKey returns the AES key for external file decryption (e.g., download tool).
// This allows tools to decrypt WeChat files without accessing the full msgCrypt.
func (mc *msgCrypt) GetAESKey() []byte {
	return mc.aesKey
}

// ParseEncodingAESKey parses the 43-character EncodingAESKey into a 32-byte AES key.
// This is a public helper for tools that need to decrypt WeChat files independently.
//
// The EncodingAESKey is a 43-character base64 string (without trailing '=').
// After adding '=' and decoding, it becomes a 32-byte AES-256 key.
func ParseEncodingAESKey(encodingAESKey string) ([]byte, error) {
	if len(encodingAESKey) != encodingAESKeyLength {
		return nil, fmt.Errorf(
			"wecom: encoding_aes_key must be 43 chars, got %d",
			len(encodingAESKey),
		)
	}
	aesKey, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil {
		return nil, fmt.Errorf("wecom: decode encoding_aes_key: %w", err)
	}
	if len(aesKey) != aesKeySize {
		return nil, fmt.Errorf(
			"wecom: decoded aes key must be 32 bytes, got %d",
			len(aesKey),
		)
	}
	return aesKey, nil
}

func decryptFileWithEncodingAESKey(
	encodingAESKey string,
	encryptedData []byte,
) ([]byte, error) {
	aesKey, err := ParseEncodingAESKey(encodingAESKey)
	if err != nil {
		return nil, err
	}
	return DecryptFileWithKey(aesKey, encryptedData)
}

// DecryptFileWithKey decrypts WeChat encrypted file data using the provided AES key.
// This is a standalone function for tools that need to decrypt files independently.
//
// Parameters:
//   - aesKey: 32-byte AES-256 key (from ParseEncodingAESKey)
//   - encryptedData: Raw encrypted bytes downloaded from WeChat file URL
//
// Returns the decrypted file content.
//
// Reference: https://developer.work.weixin.qq.com/document/path/100719#文件
func DecryptFileWithKey(aesKey, encryptedData []byte) ([]byte, error) {
	if len(aesKey) != aesKeySize {
		return nil, fmt.Errorf(
			"wecom file decrypt: aes key must be 32 bytes, got %d",
			len(aesKey),
		)
	}
	if len(encryptedData) == 0 {
		return nil, errors.New("wecom file decrypt: empty data")
	}
	if len(encryptedData)%aes.BlockSize != 0 {
		return nil, fmt.Errorf(
			"wecom file decrypt: data length (%d) not multiple "+
				"of block size (%d)",
			len(encryptedData),
			aes.BlockSize,
		)
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("wecom file decrypt: new cipher: %w", err)
	}

	// IV = first 16 bytes of AES key
	iv := aesKey[:aes.BlockSize]
	decrypted := make([]byte, len(encryptedData))
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(decrypted, encryptedData)

	// Remove PKCS#7 padding (WeChat uses padding up to 32 bytes)
	decrypted, err = pkcs7Unpad(decrypted)
	if err != nil {
		return nil, fmt.Errorf("wecom file decrypt: unpad: %w", err)
	}

	return decrypted, nil
}

// IsWecomFileURL checks if a URL is a WeChat encrypted file URL.
// These URLs typically contain domains like wework.qpic.cn, qyapi.weixin.qq.com, etc.
//
// This is a helper for tools to decide whether to apply decryption.
func IsWecomFileURL(rawURL string) bool {
	parsedURL, err := url.Parse(rawURL)
	if err == nil && parsedURL.Hostname() != "" {
		return isWecomMediaHost(parsedURL.Hostname())
	}

	wecomPatterns := []string{
		wecomMediaHostQPic,
		wecomMediaHostAIBotImg,
		wecomMediaHostQYAPI,
	}
	lowerURL := strings.ToLower(rawURL)
	for _, pattern := range wecomPatterns {
		if strings.Contains(lowerURL, pattern) {
			return true
		}
	}
	return false
}

func isWecomMediaHost(host string) bool {
	normalizedHost := strings.ToLower(strings.TrimSpace(host))
	if normalizedHost == "" {
		return false
	}
	if strings.Contains(normalizedHost, wecomMediaHostAIBotImg) {
		return true
	}

	allowedHosts := []string{
		wecomMediaHostQPic,
		wecomMediaHostQYAPI,
	}
	for _, allowedHost := range allowedHosts {
		if normalizedHost == allowedHost ||
			strings.HasSuffix(normalizedHost, "."+allowedHost) {
			return true
		}
	}
	return false
}

// pkcs7Unpad removes PKCS#7 padding.
// IMPORTANT: WeChat uses AES-256 (32-byte key), so padding value can be 1-32.
// Standard PKCS#7 for AES-128 (16-byte block) would limit to 1-16, but WeChat's
// C++ implementation uses kAesKeySize (32) as the upper bound for padding validation.
// Reference: WXBizJsonMsgCrypt.cpp line 238:
//
//	if (out[iSize - 1] > 0 && out[iSize - 1] <= kAesKeySize && ...)
func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}
	padding := int(data[len(data)-1])
	// WeChat uses kAesKeySize (32) as max padding, not aes.BlockSize (16)
	if padding < 1 || padding > aesKeySize {
		// Debug log for troubleshooting
		fmt.Printf("[wecom-crypto] pkcs7Unpad FAILED: invalid padding value %d (valid: 1-%d)\n", padding, aesKeySize)
		fmt.Printf("  data_len=%d, last_bytes=%v\n", len(data), data[max(0, len(data)-8):])
		return nil, fmt.Errorf("invalid padding value: %d", padding)
	}
	if padding > len(data) {
		return nil, errors.New("padding larger than data")
	}
	// Note: WeChat's C++ SDK does NOT validate all padding bytes, only the last byte value.
	// We follow the same lenient approach for compatibility.
	// Strict PKCS#7 would check: all bytes from data[len-padding:] == padding
	return data[:len(data)-padding], nil
}
