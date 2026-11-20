//go:build !vale_nodict

package spell

import _ "embed"

//go:embed data/en_US-web.dic
var defaultDic []byte
