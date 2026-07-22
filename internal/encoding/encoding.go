package encoding

import (
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func DecodeGB18030(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data)
	if err == nil {
		return string(decoded)
	}
	return string(data)
}
