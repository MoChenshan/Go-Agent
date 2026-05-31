package external

import "context"

//go:generate mockgen --source=magiccli_api.go --destination=magiccli_mock.go --package=external

// MagicCliAPI 魔方加密接口
type MagicCliAPI interface {
	// EncryptMagicID 加密魔方ID
	EncryptMagicID(ctx context.Context, plain string) (string, error)
}
