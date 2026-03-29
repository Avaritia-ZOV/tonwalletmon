package domain

type Transaction struct {
	AccountID  string
	TxHash     string
	Sender     string
	SenderName string
	Recipient  string
	Comment    string
	ActionType string
	Value      string
	Lt         uint64
	Timestamp  int64
	Amount     int64
}

type Cursor struct {
	AccountID string
	TxHash    string
	Lt        uint64
}

type DeliveryResult struct {
	StatusCode int
	Retry      bool
	RetryAfter int64
}
