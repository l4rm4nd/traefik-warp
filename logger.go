package traefik_warp

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

const warpModule = "github.com/l4rm4nd/traefik-warp"
const warpPlugin = "plugin-traefikwarp"

// global debug flag (0 = off, 1 = on)
var debugEnabled uint32

func enableDebug(on bool) {
	if on {
		atomic.StoreUint32(&debugEnabled, 1)
	} else {
		atomic.StoreUint32(&debugEnabled, 0)
	}
}

// Public helpers (no-op unless debug is enabled)
func logInfo(msg string, kv ...string)  { if atomic.LoadUint32(&debugEnabled) == 1 { logKV("INF", msg, kv...) } }
func logWarn(msg string, kv ...string)  { if atomic.LoadUint32(&debugEnabled) == 1 { logKV("WRN", msg, kv...) } }
func logError(msg string, kv ...string) { if atomic.LoadUint32(&debugEnabled) == 1 { logKV("ERR", msg, kv...) } }

// Internal formatter: Traefik-like "TIMESTAMP LEVEL message k=v ... module=... plugin=..."
func logKV(level, msg string, kv ...string) {
	ts := time.Now().Format("2006-01-02T15:04:05-07:00")

	var pairs []string
	for i := 0; i+1 < len(kv); i += 2 {
		k := strings.TrimSpace(kv[i])
		v := strings.TrimSpace(kv[i+1])
		if k == "" {
			continue
		}
		v = strings.ReplaceAll(v, "\n", "")
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
	}
	pairs = append(pairs, "module="+warpModule, "plugin="+warpPlugin)

	fmt.Fprintf(os.Stdout, "%s %s %s %s\n", ts, level, msg, strings.Join(pairs, " "))
}
