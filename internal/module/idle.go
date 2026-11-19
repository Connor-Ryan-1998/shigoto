package module

import (
	"io"
	"net/http"
	"time"
)

// uploadIdleTimeout is how long an upload may go without sending any bytes
// before the server drops it. Slow-but-moving uploads are never cut off;
// stalled ones don't hold a goroutine and staging file indefinitely.
const uploadIdleTimeout = 60 * time.Second

// idleReader pushes the connection's read deadline out by idle before each
// Read, so the deadline bounds silence rather than total duration.
type idleReader struct {
	r     io.Reader
	idle  time.Duration
	now   func() time.Time
	setRD func(time.Time) error
}

func (i *idleReader) Read(p []byte) (int, error) {
	_ = i.setRD(i.now().Add(i.idle)) // unsupported writers (tests) just skip it
	return i.r.Read(p)
}

// withIdleDeadline wraps r.Body so a stalled upload times out after
// uploadIdleTimeout of silence.
func withIdleDeadline(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	r.Body = struct {
		io.Reader
		io.Closer
	}{&idleReader{r: r.Body, idle: uploadIdleTimeout, now: time.Now, setRD: rc.SetReadDeadline}, r.Body}
}
