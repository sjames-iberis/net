package rfc6242

import (
	"io"
	"testing"
)

var EOM = string(tokenEOM)

func TestEOMDecoding(t *testing.T) {

	type decresp struct {
		inputs      []string
		buffer string
		err    error
	}

	tests := []struct {
		name      string
		buflen    int
		responses []decresp
	}{
		{"MessageWithEOM", 100,
			[]decresp{
				{[]string{"123456_abcde" + EOM},"123456_abcde", nil},
				{[]string{"XYZ1" + EOM} ,"XYZ1", nil},
				{nil, "", io.EOF},
			},
		},
		{"SeparatePayload_EOM", 100,
			[]decresp{
				{[]string{"123456_abcde", EOM}, "123456_abcde", nil},
				{[]string{"XYZ1", EOM},"XYZ1", nil},
				{nil,"", io.EOF},
			},
		},
		{"MessageSplitOverBuffer", 7,
			[]decresp{
				{ []string{"1234567"},"1234567", nil},
				{[]string{"AB", EOM}, "AB", nil},
				{[]string{"abcdefg"},"abcdefg", nil},
				{[]string{"h", EOM}, "h", nil},
				{nil, "", io.EOF},
			},
		},
		{"InputTooLongForBuffer", 8,

			[]decresp{
				{[]string{"1234567890" + EOM}, "12345678", nil},
				{nil,"90", nil},
			},
		},
		{"PartialEOM", 100,
			[]decresp{
				{ []string{"1234]]>]]XYZ" + EOM}, "1234]]>]]XYZ", nil},
				{nil, "", io.EOF},
			},
		},
		{"SmallWrites", 100,
			[]decresp{
				{ []string{"AB", "CD", "EF"}, "ABCDEF", nil},
				{ []string{"G", EOM}, "G", nil},
				{nil, "", io.EOF},
			},
		},
		{"MissingEOM", 100,
			[]decresp{
				{[]string{"ABCDEF"}, "ABCDEF", nil},
				{nil,"", io.ErrUnexpectedEOF},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			transport := newTransport()

			d := NewDecoder(transport.r)

			buffer := make([]byte, tt.buflen)
			for i, resp := range tt.responses {
				transport.Write(resp.inputs)

				count, err := d.Read(buffer)
				token := string(buffer[:count])
				if resp.buffer != token {
					t.Errorf("Decoder %s[%d]: buffer mismatch wanted >%s< got >%s<", tt.name, i, resp.buffer, token)
				} else if resp.err != err {
					t.Errorf("Decoder %s[%d]: error mismatch wanted %s got %s", tt.name, i, resp.err, err)
				}
			}

		})
	}
}

func TestFramerTransition(t *testing.T) {

	type decresp struct {
		inputs      []string
		buffer     string
		err        error
		setChunked bool
	}

	tests := []struct {
		name   string
		buflen int

		responses []decresp
	}{
		{"SimpleSwitch", 100,
			[]decresp{
				{[]string{"<hello/>" + EOM}, "<hello/>", nil, true},
				{[]string{"\n#6\n", "<rpc/>", "\n##\n"},"<rpc/>", nil, false}, // Multiple writes
				{nil, "", io.EOF, false},
			},
		},
		{"SwitchWithDanglingEOM", 100,
			[]decresp{
				{[]string{"<hello/>"}, "<hello/>", nil, true},
				{[]string{EOM + "\n#6\n" + "<rpc/>" + "\n##\n"}, "<rpc/>", nil, false},  // Single write
				{nil, "", io.EOF, false},
			},
		},
		{"SplitChunkMetadata", 100,
			[]decresp{
				{[]string{"<hello/>" + EOM}, "<hello/>", nil, true},
				{[]string{"\n#6", "\n" + "<rpc/>" + "\n#", "#\n"}, "<rpc/>", nil, false},  // Single write
				{nil, "", io.EOF, false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			transport := newTransport()

			d := NewDecoder(transport.r)

			buffer := make([]byte, tt.buflen)
			for i, resp := range tt.responses {

				transport.Write(resp.inputs)

				count, err := d.Read(buffer)
				token := string(buffer[:count])
				if resp.buffer != token {
					t.Errorf("Decoder %s[%d]: buffer mismatch wanted >%s< got >%s<", tt.name, i, resp.buffer, token)
				} else if resp.err != err {
					t.Errorf("Decoder %s[%d]: error mismatch wanted %s got %s", tt.name, i, resp.err, err)
				}
				if resp.setChunked {
					SetChunkedFraming(d)
				}
			}
		})
	}
}

func newTransport() (*transport) {
	pr, pw := io.Pipe()
	t := &transport{r: pr, w: pw, ch: make(chan string, 5)}
	go func() {
		for s := range t.ch {
			t.w.Write([]byte(s))
		}
		t.w.Close()
	}()
	return t
}

type transport struct {
	r io.Reader
	w io.WriteCloser
	ch chan string
}

func (t *transport) Write(inputs []string) {

	if inputs == nil {
		close(t.ch)
	} else {
		for _, s := range inputs {
			t.ch <- s
		}
	}
}

//func (t *transport) Close() {
//	go func() { t.w.Close() }()
//}

func reader(resps []string) *dummyReader {
	return &dummyReader{responses: resps}
}

type dummyReader struct {
	responses []string
	idx       int
}

func (dr *dummyReader) Read(p []byte) (int, error) {

	if dr.idx >= len(dr.responses) {
		return 0, io.EOF
	}
	src := dr.responses[dr.idx]
	l := copy(p, src)
	dr.idx++
	return l, nil
}
