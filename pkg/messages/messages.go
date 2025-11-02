package messages

// ConnectedPayload is sent from server to client when a connection is
// successfully established. It contains the assigned client id.
type ConnectedPayload struct {
    ClientID string `json:"client_id"`
}


type NotificationPayload struct {
	MsgID    string `json:"msg_id"`
	Data     string `json:"data"`
	ClientID string `json:"client_id"`
}