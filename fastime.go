package fastime

import (
	"bytes"
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

type Fastime interface {
	IsDaemonRunning() bool
	GetFormat() string
	SetFormat(format string) Fastime
	GetLocation() *time.Location
	SetLocation(location *time.Location) Fastime
	Now() time.Time
	Stop()
	UnixNow() int64
	UnixUNow() uint32
	UnixNanoNow() int64
	UnixUNanoNow() uint32
	FormattedNow() []byte
	StartTimerD(ctx context.Context, dur time.Duration) Fastime
}

// Fastime is fastime's base struct, it's stores atomic time object
type fastime struct {
	uut           uint32
	uunt          uint32
	dur           int64
	ut            int64
	unt           int64
	correctionDur time.Duration
	running       *atomic.Value
	t             *atomic.Value
	ft            *atomic.Value
	format        *atomic.Value
	location      *atomic.Value
	cancel        context.CancelFunc
	pool          sync.Pool
}

// New returns Fastime
func New() Fastime {
	return newFastime()
}

func newFastime() *fastime {
	f := &fastime{
		t: new(atomic.Value),
		running: func() *atomic.Value {
			av := new(atomic.Value)
			av.Store(false)
			return av
		}(),
		ut:   math.MaxInt64,
		unt:  math.MaxInt64,
		uut:  math.MaxUint32,
		uunt: math.MaxUint32,
		format: func() *atomic.Value {
			av := new(atomic.Value)
			av.Store(time.RFC3339)
			return av
		}(),
		location: func() *atomic.Value {
			av := new(atomic.Value)
			av.Store(time.Local)
			return av
		}(),
		correctionDur: time.Millisecond * 100,
	}
	f.pool = sync.Pool{
		New: func() interface{} {
			return bytes.NewBuffer(make([]byte, 0, len(f.GetFormat())))
		},
	}
	f.ft = func() *atomic.Value {
		av := new(atomic.Value)
		buf := f.pool.Get().(*bytes.Buffer)
		buf.Reset()
		av.Store(buf.Bytes())
		f.pool.Put(buf)
		return av
	}()

	return f.refresh()
}

func (f *fastime) update() *fastime {
	return f.store(f.Now().Add(time.Duration(atomic.LoadInt64(&f.dur))))
}

func (f *fastime) refresh() *fastime {
	return f.store(f.now())
}

func (f *fastime) store(t time.Time) *fastime {
	f.t.Store(t)
	ut := t.Unix()
	unt := t.UnixNano()
	atomic.StoreInt64(&f.ut, ut)
	atomic.StoreInt64(&f.unt, unt)
	atomic.StoreUint32(&f.uut, *(*uint32)(unsafe.Pointer(&ut)))
	atomic.StoreUint32(&f.uunt, *(*uint32)(unsafe.Pointer(&unt)))
	form := f.GetFormat()
	buf := f.pool.Get().(*bytes.Buffer)
	if buf == nil || len(form) > buf.Cap() {
		buf.Grow(len(form))
	}
	f.ft.Store(t.AppendFormat(buf.Bytes()[:0], form))
	f.pool.Put(buf)
	return f
}

func (f *fastime) IsDaemonRunning() bool {
	return f.running.Load().(bool)
}

func (f *fastime) GetLocation() *time.Location {
	return f.location.Load().(*time.Location)
}

func (f *fastime) GetFormat() string {
	return f.format.Load().(string)
}

// SetLocation replaces time location
func (f *fastime) SetLocation(location *time.Location) Fastime {
	f.location.Store(location)
	f.refresh()
	return f
}

// SetFormat replaces time format
func (f *fastime) SetFormat(format string) Fastime {
	f.format.Store(format)
	f.refresh()
	return f
}

// Now returns current time
func (f *fastime) Now() time.Time {
	return f.t.Load().(time.Time)
}

// Stop stops stopping time refresh daemon
func (f *fastime) Stop() {
	if f.IsDaemonRunning() {
		f.cancel()
		atomic.StoreInt64(&f.dur, 0)
		return
	}
}

// UnixNow returns current unix time
func (f *fastime) UnixNow() int64 {
	return atomic.LoadInt64(&f.ut)
}

// UnixNow returns current unix time
func (f *fastime) UnixUNow() uint32 {
	return atomic.LoadUint32(&f.uut)
}

// UnixNanoNow returns current unix nano time
func (f *fastime) UnixNanoNow() int64 {
	return atomic.LoadInt64(&f.unt)
}

// UnixNanoNow returns current unix nano time
func (f *fastime) UnixUNanoNow() uint32 {
	return atomic.LoadUint32(&f.uunt)
}

// FormattedNow returns formatted byte time
func (f *fastime) FormattedNow() []byte {
	return f.ft.Load().([]byte)
}

// StartTimerD provides time refresh daemon
func (f *fastime) StartTimerD(ctx context.Context, dur time.Duration) Fastime {
	if f.IsDaemonRunning() {
		f.Stop()
	}
	f.refresh()

	var ct context.Context
	ct, f.cancel = context.WithCancel(ctx)
	go func() {
		f.running.Store(true)
		f.dur = math.MaxInt64
		atomic.StoreInt64(&f.dur, dur.Nanoseconds())
		ticker := time.NewTicker(time.Duration(atomic.LoadInt64(&f.dur)))
		ctick := time.NewTicker(f.correctionDur)
		for {
			select {
			case <-ct.Done():
				f.running.Store(false)
				ticker.Stop()
				ctick.Stop()
				return
			case <-ticker.C:
				f.update()
			case <-ctick.C:
				select {
				case <-ct.Done():
					f.running.Store(false)
					ticker.Stop()
					ctick.Stop()
					return
				case <-ticker.C:
					f.refresh()
				}
			}
		}
	}()
	return f
}
