//go:build vale_nodict

// The build without the default dictionary: a spelling rule has to name its
// own, and one that relies on the default reports ErrNoDictionary.
package spell

var defaultDic []byte
