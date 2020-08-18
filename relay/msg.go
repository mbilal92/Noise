package relay

import (
	"encoding/hex"
	"strconv"

	"github.com/mbilal92/noise"
	"github.com/mbilal92/noise/payload"
)

type Message struct {
	From    noise.ID
	Code    byte
	randomN uint32
	To      noise.PublicKey
	Data    []byte
	ChainID string
}

func (msg Message) Marshal() []byte {
	writer := payload.NewWriter(nil)
	writer.Write(msg.From.Marshal())
	writer.WriteByte(msg.Code)
	writer.WriteUint32(msg.randomN)
	writer.WriteString(msg.ChainID)
	writer.WriteBytes(msg.To[:])
	// writer.WriteUint32(uint32(len(msg.To[:])))
	// writer.Write([]byte(msg.To[:]))
	// writer.WriteUint32(uint32(len(msg.Data)))
	// writer.Write(msg.Data)
	writer.WriteBytes(msg.Data)
	return writer.Bytes()
}

func (m Message) String() string {
	var msg string
	if len(m.Data) > 30 {
		msg = hex.EncodeToString(m.Data[:30]) + "...." + hex.EncodeToString(m.Data[len(m.Data)-30:])
	} else {
		msg = hex.EncodeToString(m.Data)
	}
	return " From " + m.From.String() + " To:" + m.To.String() + "SeqNum: " + strconv.FormatUint(uint64(m.randomN), 10) + " Code: " + strconv.Itoa(int(m.Code)) + " ChainID: " + m.ChainID + " msg: " + msg + "\n"
}

func UnmarshalMessage(buf []byte) (Message, error) {
	// fmt.Println("Relay Message Unmarshal")
	msg := Message{}
	msg.From, _ = noise.UnmarshalID(buf)

	buf = buf[msg.From.Size():]
	reader := payload.NewReader(buf)
	code, err := reader.ReadByte()
	if err != nil {
		panic(err)
	}
	msg.Code = code

	randomN, err := reader.ReadUint32()
	if err != nil {
		panic(err)
	}
	msg.randomN = randomN

	chainID, err := reader.ReadString()
	if err != nil {
		panic(err)
	}
	msg.ChainID = chainID

	to, err := reader.ReadBytes()
	if err != nil {
		panic(err)
	}
	copy(msg.To[:], to)

	data, err := reader.ReadBytes()
	if err != nil {
		panic(err)
	}
	msg.Data = data
	return msg, nil
}
