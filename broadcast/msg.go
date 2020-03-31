package broadcast

import (
	"github.com/mbilal92/noise"
	"github.com/mbilal92/noise/payload"
)

type Message struct {
	From noise.ID
	Data []byte
}

func (msg Message) Marshal() []byte {
	writer := payload.NewWriter(nil)
	writer.Write(msg.From.Marshal())
	writer.WriteUint32(uint32(len(msg.Data)))
	writer.Write(msg.Data)
	return writer.Bytes()
}

func (m Message) String() string {
	return "\nFrom " + m.From.String() + " msg: " + string(m.Data) + "\n"
}

func UnmarshalMessage(buf []byte) (Message, error) {
	// fmt.Println("Relay Message Unmarshal")
	msg := Message{}
	msg.From, _ = noise.UnmarshalID(buf)

	buf = buf[msg.From.Size():]
	reader := payload.NewReader(buf)

	data, err := reader.ReadBytes()
	if err != nil {
		panic(err)
	}
	msg.Data = data
	return msg, nil
}
