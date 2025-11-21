package nblog

import (
	"log/slog"

	jsoniter "github.com/json-iterator/go"
)

func writeAttribute(out *jsonStream, h *baseHandler, groups []string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	switch attr.Value.Kind() {
	case slog.KindGroup:
		writeGroup(out, h, groups, attr)
	default:
		write, ok := writeByKind[attr.Value.Kind()]
		if !ok {
			panic("No writer found for value")
		}
		write(out, attr)
	}
}

func writeString(out *jsonStream, attr slog.Attr) {
	out.WriteObjectField(attr.Key)
	out.WriteString(attr.Value.String())
}

func writeInt64(out *jsonStream, attr slog.Attr) {
	out.WriteObjectField(attr.Key)
	out.WriteInt64(attr.Value.Int64())
}

func writeUint64(out *jsonStream, attr slog.Attr) {
	out.WriteObjectField(attr.Key)
	out.WriteUint64(attr.Value.Uint64())
}

func writeFloat64(out *jsonStream, attr slog.Attr) {
	out.WriteObjectField(attr.Key)
	out.WriteFloat64(attr.Value.Float64())
}

func writeBool(out *jsonStream, attr slog.Attr) {
	out.WriteObjectField(attr.Key)
	out.WriteBool(attr.Value.Bool())
}

func writeDuration(out *jsonStream, attr slog.Attr) {
	out.WriteObjectField(attr.Key)
	out.WriteString(attr.Value.Duration().String())
}

func writeTime(out *jsonStream, attr slog.Attr) {
	out.WriteObjectField(attr.Key)
	out.WriteString(attr.Value.Time().String())
}

func writeAny(out *jsonStream, attr slog.Attr) {
	out.WriteObjectField(attr.Key)
	out.WriteVal(attr.Value.Any())
}

func writeLogValuer(*jsonStream, slog.Attr) {
	panic("Unexpected use of LogValuer instead of Value.Resolve")
}

func writeGroup(out *jsonStream, base *baseHandler, groups []string, attr slog.Attr) {
	if attr.Key != "" {
		out.WriteObjectField(attr.Key)
		out.WriteObjectStart()
		groups = append(groups, attr.Key)
		defer out.WriteObjectEnd()
	}
	for _, at := range attr.Value.Group() {
		_ = base.writeNextAttribute(at, out, groups)
	}
}

var writeByKind = map[slog.Kind]func(*jsonStream, slog.Attr){
	slog.KindString:    writeString,
	slog.KindInt64:     writeInt64,
	slog.KindUint64:    writeUint64,
	slog.KindFloat64:   writeFloat64,
	slog.KindBool:      writeBool,
	slog.KindDuration:  writeDuration,
	slog.KindTime:      writeTime,
	slog.KindAny:       writeAny,
	slog.KindLogValuer: writeLogValuer,
}

type jsonStream struct {
	stream    *jsoniter.Stream
	needComma bool
}

func newJSONStream() *jsonStream {
	const jsonBufferSize = 50 // size is arbitrary
	return &jsonStream{
		stream:    jsoniter.NewStream(jsoniter.Config{}.Froze(), nil, jsonBufferSize),
		needComma: false,
	}
}

func (js *jsonStream) WriteObjectField(label string) {
	if js.needComma {
		js.stream.WriteMore()
		js.stream.WriteRaw(" ")
	}
	js.stream.WriteObjectField(label)
	js.stream.WriteRaw(" ")
	js.needComma = true
}

func (js *jsonStream) WriteObjectStart() {
	js.stream.WriteObjectStart()
	js.needComma = false
}

func (js *jsonStream) WriteObjectEnd() {
	js.stream.WriteObjectEnd()
	js.needComma = true
}

func (js *jsonStream) WriteBool(val bool) {
	js.stream.WriteBool(val)
}

func (js *jsonStream) WriteFloat64(val float64) {
	js.stream.WriteFloat64(val)
}

func (js *jsonStream) WriteInt64(val int64) {
	js.stream.WriteInt64(val)
}

func (js *jsonStream) WriteRaw(s string) {
	js.stream.WriteRaw(s)
}

func (js *jsonStream) WriteString(val string) {
	js.stream.WriteString(val)
}

func (js *jsonStream) WriteUint64(val uint64) {
	js.stream.WriteUint64(val)
}

func (js *jsonStream) WriteVal(val any) {
	js.stream.WriteVal(val)
}

func (js *jsonStream) Error() error {
	return js.stream.Error
}

func (js *jsonStream) Buffer() []byte {
	return js.stream.Buffer()
}
