package domain

// Setting is one stored panel setting; secret ones are sealed (Cipher set).
type Setting struct {
	Key    string
	Value  string
	Cipher []byte
	Nonce  []byte
	KeyID  string
}
