package lora

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/Sirupsen/logrus"
	loraserver_structs "github.com/brocaar/lora-gateway-bridge/gateway"
	"github.com/brocaar/lorawan"
	"lorhammer/src/model"
	"lorhammer/src/tools"
	"math"
	"time"
)

type Packet struct {
	Rxpk []loraserver_structs.RXPK `json:"rxpk,omitempty"`
}

func NewRxpk(data []byte, gateway *model.Gateway) loraserver_structs.RXPK {

	rxpk := loraserver_structs.RXPK{
		Tmst: 123456,
		Freq: 866.349812,
		Chan: 2,
		RFCh: 0,
		Stat: 1,
		Modu: "LORA",
		DatR: loraserver_structs.DatR{LoRa: "SF7BW125"},
		CodR: "4/6",
		RSSI: -35,
		LSNR: 5.1,
		Size: uint16(len(data)),
		Data: base64.StdEncoding.EncodeToString(data),
	}

	if gateway.RxpkDate > 0 {
		rxpk.Time = loraserver_structs.CompactTime(time.Unix(gateway.RxpkDate, 0).UTC())
	} else {
		rxpk.Time = loraserver_structs.CompactTime(time.Now().UTC())
	}
	return rxpk
}

func (p Packet) Prepare(gateway *model.Gateway) ([]byte, error) {

	//payload
	payload, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}

	//generate token
	token := make([]byte, 2)
	binary.LittleEndian.PutUint16(token, uint16(tools.Random(math.MinInt16, math.MaxUint16)))

	packet := make([]byte, 0)
	packet = append(packet, loraserver_structs.ProtocolVersion2)
	packet = append(packet, token...)
	packet = append(packet, byte(loraserver_structs.PushData))
	packet = append(packet, gateway.MacAddress[0:]...)
	packet = append(packet, payload...)

	return packet, nil
}

func HandlePacket(data []byte) error {

	pt, err := loraserver_structs.GetPacketType(data)
	if err != nil {
		return err
	}

	switch pt {
	case loraserver_structs.PullACK:
		return handlePullAck(data)
	case loraserver_structs.PushACK:
		return handlePushAck(data)
	case loraserver_structs.PullResp:
		return handlePullRespPacket(data)
	default:
		logrus.WithFields(logrus.Fields{
			"ref":        "lorhammer/lorapacket:HandlePacket()",
			"packetType": pt,
		}).Error("gateway: unknown packet type")
		return nil
	}
	return nil
}

func handlePullRespPacket(data []byte) error {

	logrus.WithFields(logrus.Fields{
		"ref":  "lorhammer/lorapacket:HandlePacket()",
		"type": "pullResp",
	}).Info("gateway: received udp packet from NS")

	var pullRespPacket loraserver_structs.PullRespPacket
	err := pullRespPacket.UnmarshalBinary(data)
	if err != nil {
		return errors.New("Error marshalling ")
	}

	payloadBytes, err := base64.StdEncoding.DecodeString(pullRespPacket.Payload.TXPK.Data)
	if err != nil {
		return errors.New("Can't Decode base64 JoinAccept Data")
	}

	var phyPayload lorawan.PHYPayload
	phyPayload.UnmarshalBinary(payloadBytes)

	return nil

}

func handlePushAck(data []byte) error {

	var pushAckPacket loraserver_structs.PushACKPacket
	err := pushAckPacket.UnmarshalBinary(data)
	if err != nil {
		return errors.New("couldn't unmarshall pushAckPacket")
	}

	logrus.WithFields(logrus.Fields{
		"ref":              "lorhammer/lorapacket:HandlePacket()",
		"type":             "pushAck",
		"protocol_version": data[0],
		"randomTocken":     pushAckPacket.RandomToken,
	}).Info("gateway: received udp packet from NS")

	return nil
}

func handlePullAck(data []byte) error {

	var pullAckPacket loraserver_structs.PullACKPacket
	err := pullAckPacket.UnmarshalBinary(data)

	if err != nil {
		return errors.New("couldn't unmarshall pullAckPacket")
	}

	logrus.WithFields(logrus.Fields{
		"ref":              "lorhammer/lorapacket:HandlePacket()",
		"type":             "pullAck",
		"protocol_version": data[0],
		"randomTocken":     pullAckPacket.RandomToken,
	}).Info("gateway: received udp packet from NS")

	return nil
}
