package webhook

import (
	"strconv"

	"ton-monitoring/internal/domain"
)

func BuildPayload(buf []byte, tx domain.Transaction) []byte {
	buf = append(buf, `{"event":"transaction","account_id":"`...)
	buf = appendJSONString(buf, tx.AccountID)
	buf = append(buf, `","tx_hash":"`...)
	buf = appendJSONString(buf, tx.TxHash)
	buf = append(buf, `","action":"`...)
	buf = appendJSONString(buf, tx.ActionType)
	buf = append(buf, `","value":"`...)
	buf = appendJSONString(buf, tx.Value)
	buf = append(buf, `","sender":"`...)
	buf = appendJSONString(buf, tx.Sender)
	buf = append(buf, `","sender_name":"`...)
	buf = appendJSONString(buf, tx.SenderName)
	buf = append(buf, `","recipient":"`...)
	buf = appendJSONString(buf, tx.Recipient)
	buf = append(buf, `","amount_nano":`...)
	buf = strconv.AppendInt(buf, tx.Amount, 10)
	buf = append(buf, `,"comment":"`...)
	buf = appendJSONString(buf, tx.Comment)
	buf = append(buf, `","lt":`...)
	buf = strconv.AppendUint(buf, tx.Lt, 10)
	buf = append(buf, `,"timestamp":`...)
	buf = strconv.AppendInt(buf, tx.Timestamp, 10)
	buf = append(buf, '}')
	return buf
}

const hexDigits = "0123456789abcdef"

func appendJSONString(buf []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			buf = append(buf, '\\', '"')
		case c == '\\':
			buf = append(buf, '\\', '\\')
		case c == '\n':
			buf = append(buf, '\\', 'n')
		case c == '\r':
			buf = append(buf, '\\', 'r')
		case c == '\t':
			buf = append(buf, '\\', 't')
		case c < 0x20:
			buf = append(buf, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
		default:
			buf = append(buf, c)
		}
	}
	return buf
}
