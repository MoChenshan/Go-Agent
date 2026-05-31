package pcg123

// Config is the config of the code executor
type Config struct {
	Language  Language `validate:"required"` // the language of the code executor, required
	SecretID  string   `validate:"required"` // the secret id of the code executor, required
	SecretKey string   `validate:"required"` // the secret key of the code executor, required
}
