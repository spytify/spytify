package settings

//go:generate go run ../tools/genkeys -o keys_gen.go

// Key names a setting for Values' getters. keys_gen.go holds one per row of
// Table, generated from it, and no other package can make one: a string,
// even a misspelt key, does not convert to a Key. So a misspelt or removed
// setting fails the build instead of a recording.
type Key struct{ name string }

// String is the key as config.toml and the protocol spell it.
func (k Key) String() string { return k.name }
