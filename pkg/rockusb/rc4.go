package rockusb

// RC4Key is the Rockchip maskrom RC4 encryption key used for SDRAM writes.
var RC4Key = []byte{
	0x7C, 0x4E, 0x03, 0x04, 0x55, 0x05, 0x09, 0x07,
	0x2D, 0x2C, 0x7B, 0x38, 0x17, 0x0D, 0x17, 0x11,
}

// rc4State holds the RC4 cipher state.
type rc4State struct {
	s    [256]byte
	i, j byte
}

// newRC4 initializes an RC4 cipher with the given key.
func newRC4(key []byte) *rc4State {
	var s rc4State
	for i := 0; i < 256; i++ {
		s.s[i] = byte(i)
	}
	var j byte
	for i := 0; i < 256; i++ {
		j += s.s[i] + key[i%len(key)]
		s.s[i], s.s[j] = s.s[j], s.s[i]
	}
	return &s
}

// xor encrypts/decrypts data in place using RC4.
func (s *rc4State) xor(data []byte) {
	for k := range data {
		s.i++
		s.j += s.s[s.i]
		s.s[s.i], s.s[s.j] = s.s[s.j], s.s[s.i]
		data[k] ^= s.s[s.s[s.i]+s.s[s.j]]
	}
}

// RC4Encrypt encrypts data using the Rockchip RC4 key.
// Returns a new encrypted copy; does not modify the input.
func RC4Encrypt(data []byte) []byte {
	out := make([]byte, len(data))
	copy(out, data)
	cipher := newRC4(RC4Key)
	cipher.xor(out)
	return out
}
