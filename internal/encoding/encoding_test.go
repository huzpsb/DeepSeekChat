package encoding

import (
	"testing"
)

func TestDecodeGB18030_ValidUTF8(t *testing.T) {
	got := DecodeGB18030([]byte("hello world"))
	if got != "hello world" {
		t.Fatalf("expected 'hello world', got %q", got)
	}
}

func TestDecodeGB18030_Chinese(t *testing.T) {
	got := DecodeGB18030([]byte{0xc4, 0xe3, 0xba, 0xc3, 0xca, 0xc0, 0xbd, 0xe7})
	if got != "你好世界" {
		t.Fatalf("expected '你好世界', got %q", got)
	}
}

func TestDecodeGB18030_UTF8Already(t *testing.T) {
	input := []byte("你好世界")
	got := DecodeGB18030(input)
	if got != "你好世界" {
		t.Fatalf("expected '你好世界', got %q", got)
	}
}
