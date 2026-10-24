package carriers

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// Reporter receives progress events from a downloader.
type Reporter interface {
	// Stage starts a numbered pipeline stage.
	Stage(n, of int, name string)
	// Info reports a line of detail within the current stage.
	Info(format string, args ...any)
	// Warn reports something wrong but not fatal.
	Warn(format string, args ...any)
}

// Console prints progress lines with elapsed-time stamps. Safe for
// concurrent use; embed it to add source-specific events.
type Console struct {
	Mu       sync.Mutex
	W        io.Writer
	Start    time.Time
	Warnings int
}

// NewConsole returns a Console writing to w, timed from now.
func NewConsole(w io.Writer) *Console { return &Console{W: w, Start: time.Now()} }

// TS is the elapsed-time stamp.
func (c *Console) TS() string { return fmt.Sprintf("[%6.1fs]", time.Since(c.Start).Seconds()) }

func (c *Console) Stage(n, of int, name string) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	fmt.Fprintf(c.W, "\n%s ==> [%d/%d] %s\n", c.TS(), n, of, name)
}

func (c *Console) Info(format string, args ...any) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	fmt.Fprintf(c.W, "%s     %s\n", c.TS(), fmt.Sprintf(format, args...))
}

func (c *Console) Warn(format string, args ...any) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.Warnings++
	fmt.Fprintf(c.W, "%s   ! %s\n", c.TS(), fmt.Sprintf(format, args...))
}

// Progress prints a "[done/total pct%] line" progress row.
func (c *Console) Progress(done, total int, format string, args ...any) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	fmt.Fprintf(c.W, "%s     [%3d/%d %3d%%] %s\n", c.TS(), done, total, 100*done/max(total, 1), fmt.Sprintf(format, args...))
}

// PrintStats prints the HTTP/cache lines of a run summary.
func PrintStats(w io.Writer, s *Stats, warnings int) {
	fmt.Fprintf(w, "  http           %d requests, %s downloaded, %d retries, %d not found\n",
		s.Requests.Load(), FmtBytes(s.Bytes.Load()), s.Retries.Load(), s.NotFound.Load())
	fmt.Fprintf(w, "  cache          %d hits, %s\n", s.CacheHits.Load(), FmtBytes(s.CacheBytes.Load()))
	fmt.Fprintf(w, "  warnings       %d\n", warnings)
}
