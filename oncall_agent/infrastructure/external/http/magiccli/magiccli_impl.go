package magiccli

import (
	"context"

	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.code.oa.com/trpcprotocol/magic_frame/crypto"
)

// magicCliImpl 魔方CLI客户端实现
type magicCliImpl struct{}

// EncryptMagicID 加密magicID
func (m *magicCliImpl) EncryptMagicID(ctx context.Context, plain string) (string, error) {
	cipherText, err := crypto.NewCryptoClientProxy().EnCode(ctx, &crypto.EncryptoReq{
		Plaintext: plain,
	})
	if err != nil {
		log.ErrorContextf(ctx, "EnCode err, plain: %s, err: %+v", plain, err)
		return "", err
	}
	return cipherText.Ciphertext, nil
}
