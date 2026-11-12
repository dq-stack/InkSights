// Package krds decodes Kindle reader data store files (the .azw3f/.azw3r,
// .yjf/.yjr and .mbs/.mbp1 sidecars in a book's .sdr folder).
//
// The format is self-describing: every value starts with a type byte, and
// named objects run until an end marker. So the whole file can be read into
// a generic tree without knowing every object; InkSights then looks up the
// two objects it needs (timer.model and page.history.store).
//
// Format notes from John Howell's KRDS decoder (GPL v3,
// https://github.com/K-R-D-S/KRDS), which this replaces.
package krds

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

var signature = []byte{0, 0, 0, 0, 0, 0x1A, 0xB1, 0x26}

const (
	tBoolean     = 0
	tInt         = 1
	tLong        = 2
	tUTF         = 3
	tDouble      = 4
	tShort       = 5
	tFloat       = 6
	tByte        = 7
	tChar        = 9
	tObjectBegin = -2
	tObjectEnd   = -1
)

// Object is a named object and its values in file order.
type Object struct {
	Name   string
	Values []any
}

// Decode returns the top-level values (mostly *Object) of a sidecar file.
func Decode(data []byte) ([]any, error) {
	if !bytes.HasPrefix(data, signature) {
		return nil, errors.New("not a reader data store file (bad signature)")
	}
	d := &decoder{buf: data, off: len(signature)}
	first, err := d.next()
	if err != nil {
		return nil, err
	}
	if v, ok := AsInt64(first); !ok || v != 1 {
		return nil, fmt.Errorf("unexpected first value %v", first)
	}
	n, err := d.next()
	if err != nil {
		return nil, err
	}
	count, ok := AsInt64(n)
	if !ok || count < 0 || count > int64(len(data)) {
		return nil, fmt.Errorf("bad value count %v", n)
	}
	out := make([]any, 0, count)
	for i := int64(0); i < count; i++ {
		v, err := d.next()
		if err != nil {
			return out, fmt.Errorf("value %d at offset %d: %w", i, d.off, err)
		}
		out = append(out, v)
	}
	return out, nil
}

type decoder struct {
	buf []byte
	off int
}

func (d *decoder) take(n int) ([]byte, error) {
	if n < 0 || d.off+n > len(d.buf) {
		return nil, fmt.Errorf("truncated (need %d bytes at %d)", n, d.off)
	}
	b := d.buf[d.off : d.off+n]
	d.off += n
	return b, nil
}

func (d *decoder) typeByte() (int8, error) {
	b, err := d.take(1)
	if err != nil {
		return 0, err
	}
	return int8(b[0]), nil
}

func (d *decoder) next() (any, error) {
	t, err := d.typeByte()
	if err != nil {
		return nil, err
	}
	return d.value(t)
}

func (d *decoder) value(t int8) (any, error) {
	switch t {
	case tBoolean:
		b, err := d.take(1)
		if err != nil {
			return nil, err
		}
		switch b[0] {
		case 0:
			return false, nil
		case 1:
			return true, nil
		}
		return nil, fmt.Errorf("bad boolean %d", b[0])
	case tInt:
		b, err := d.take(4)
		if err != nil {
			return nil, err
		}
		return int32(binary.BigEndian.Uint32(b)), nil
	case tLong:
		b, err := d.take(8)
		if err != nil {
			return nil, err
		}
		return int64(binary.BigEndian.Uint64(b)), nil
	case tUTF:
		empty, err := d.value(tBoolean)
		if err != nil {
			return nil, err
		}
		if empty.(bool) {
			return "", nil
		}
		lb, err := d.take(2)
		if err != nil {
			return nil, err
		}
		s, err := d.take(int(binary.BigEndian.Uint16(lb)))
		if err != nil {
			return nil, err
		}
		return string(s), nil
	case tDouble:
		b, err := d.take(8)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(binary.BigEndian.Uint64(b)), nil
	case tShort:
		b, err := d.take(2)
		if err != nil {
			return nil, err
		}
		return int16(binary.BigEndian.Uint16(b)), nil
	case tFloat:
		b, err := d.take(4)
		if err != nil {
			return nil, err
		}
		return math.Float32frombits(binary.BigEndian.Uint32(b)), nil
	case tByte:
		b, err := d.take(1)
		if err != nil {
			return nil, err
		}
		return int8(b[0]), nil
	case tChar:
		b, err := d.take(1)
		if err != nil {
			return nil, err
		}
		return string(b), nil
	case tObjectBegin:
		name, err := d.value(tUTF)
		if err != nil {
			return nil, err
		}
		obj := &Object{Name: name.(string)}
		for {
			if d.off >= len(d.buf) {
				return nil, fmt.Errorf("object %s not closed", obj.Name)
			}
			if int8(d.buf[d.off]) == tObjectEnd {
				d.off++
				return obj, nil
			}
			v, err := d.next()
			if err != nil {
				return nil, fmt.Errorf("in %s: %w", obj.Name, err)
			}
			obj.Values = append(obj.Values, v)
		}
	}
	return nil, fmt.Errorf("unknown type %d at offset %d", t, d.off-1)
}

// Find returns the first object with this name, depth first.
func Find(values []any, name string) *Object {
	for _, v := range values {
		o, ok := v.(*Object)
		if !ok {
			continue
		}
		if o.Name == name {
			return o
		}
		if f := Find(o.Values, name); f != nil {
			return f
		}
	}
	return nil
}

// AsInt64 converts any integer value to int64.
func AsInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int32:
		return int64(n), true
	case int16:
		return int64(n), true
	case int8:
		return int64(n), true
	}
	return 0, false
}

// Timer is the book's reading timer (timer.model): a running total with no dates.
type Timer struct {
	TotalMs    int64
	TotalWords int64
}

// TimerOf reads timer.model: version, totalTime, totalWords, totalPercent, ...
func TimerOf(values []any) (Timer, bool) {
	o := Find(values, "timer.model")
	if o == nil || len(o.Values) < 3 {
		return Timer{}, false
	}
	total, ok1 := AsInt64(o.Values[1])
	words, ok2 := AsInt64(o.Values[2])
	if !ok1 {
		return Timer{}, false
	}
	if !ok2 {
		words = 0
	}
	return Timer{TotalMs: total, TotalWords: words}, true
}

// HistoryMs returns page.history.store record times (ms since the epoch):
// a count, then that many page.history.record objects of (position, time).
func HistoryMs(values []any) []int64 {
	o := Find(values, "page.history.store")
	if o == nil {
		return nil
	}
	var out []int64
	for _, v := range o.Values {
		rec, ok := v.(*Object)
		if !ok || rec.Name != "page.history.record" || len(rec.Values) < 2 {
			continue
		}
		if ms, ok := AsInt64(rec.Values[1]); ok {
			out = append(out, ms)
		}
	}
	return out
}
