package init

import "os"

func init() {
	os.DisableWritesForAppEngine = true
}
