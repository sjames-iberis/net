package rfc6242

import (
	"io"
	"testing"
)

var EOM = string(tokenEOM)

type decresp struct {
	buffer string
	err    error
}

func TestEOMDecoding(t *testing.T) {

	tests := []struct {
		name      string
		buflen    int
		inputs    []string
		responses []decresp
	}{
		{"MessageWithEOM", 100,
			[]string{
				"123456_abcde" + EOM,
				"XYZ1" + EOM,
			},
			[]decresp{
				{"123456_abcde", nil},
				{"XYZ1", nil},
				{"", io.EOF},
			},
		},
		{"SeparatePayload_EOM", 100,
			[]string{
				"123456_abcde",
				EOM,
				"XYZ1",
				EOM,
			},
			[]decresp{
				{"123456_abcde", nil},
				{"XYZ1", nil},
				{"", io.EOF},
			},
		},
		{"MessageSplitOverBuffer", 7,
			[]string{
				"1234567",
				"ABCDEF",
				EOM,
				"abcdefg",
				"hij",
				EOM,
			},
			[]decresp{
				{"1234567", nil},
				{"ABCDEF", nil},
				{"abcdefg", nil},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := reader(tt.inputs)

			d := NewDecoder(r)

			buffer := make([]byte, tt.buflen)
			for i, resp := range tt.responses {
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
