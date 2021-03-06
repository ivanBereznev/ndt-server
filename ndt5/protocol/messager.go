package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strconv"
)

// Encoding encodes the communication methods we support.
type Encoding int

// The different message types we support. This is initially Unknown for plain
// ndt5 connections and becomes JSON or TLV depending on the whether we
// receive MsgLogin or MsgExtendedLogin, but is always JSON for WS and WSS.
const (
	Unknown Encoding = iota // Unknown is the zero-value for Encoding
	JSON
	TLV
)

func (e Encoding) String() string {
	switch e {
	case Unknown:
		return "Unknown"
	case JSON:
		return "JSON"
	case TLV:
		return "TLV"
	}
	return fmt.Sprintf("Bad Encoding value: %d", int(e))
}

// Messager creates an object that can encode and decode messages in the
// corresponding format and send them along the passed-in connection.
func (e Encoding) Messager(conn Connection) Messager {
	switch e {
	case Unknown:
		log.Println("Error: Messager() called for Unknown type")
		return nil
	case JSON:
		return &jsonMessager{conn}
	case TLV:
		return &tlvMessager{conn}
	}
	log.Printf("Bad Encoding value: %d\n", int(e))
	return nil
}

// Messager allows us to send JSON and non-JSON messages using a single unified
// interface.
type Messager interface {
	SendMessage(MessageType, []byte) error
	SendS2CResults(throughputKbps, unsentBytes, totalSentBytes int64) error
	ReceiveMessage(MessageType) ([]byte, error)
	Encoding() Encoding
}

// jsonMessager has all the methods for sending JSON-format NDT messages along
// the passed-in connection.
type jsonMessager struct {
	conn Connection
}

type s2cResult struct {
	ThroughputValue  string
	UnsentDataAmount string
	TotalSentByte    string
}

func (r *s2cResult) String() string {
	b, _ := json.Marshal(r)
	return string(b)
}

func (jm *jsonMessager) SendMessage(kind MessageType, contents []byte) error {
	return SendJSONMessage(kind, string(contents), jm.conn)
}

func (jm *jsonMessager) SendS2CResults(throughputKbps, unsentBytes, totalSentBytes int64) error {
	r := &s2cResult{
		ThroughputValue:  strconv.FormatInt(throughputKbps, 10),
		UnsentDataAmount: strconv.FormatInt(unsentBytes, 10),
		TotalSentByte:    strconv.FormatInt(totalSentBytes, 10),
	}
	return WriteTLVMessage(jm.conn, TestMsg, r.String())
}

func (jm *jsonMessager) ReceiveMessage(kind MessageType) ([]byte, error) {
	msg, err := ReceiveJSONMessage(jm.conn, kind)
	if msg == nil {
		if err == nil {
			return nil, errors.New("empty message received without error")
		}
		return nil, err
	}
	return []byte(msg.Msg), err
}

func (jm *jsonMessager) Encoding() Encoding {
	return JSON
}

// tlvMessager has all the methods for sending tlv-format NDT messages along the
// passed-in connection.
type tlvMessager struct {
	conn Connection
}

func (tm *tlvMessager) SendMessage(kind MessageType, contents []byte) error {
	return WriteTLVMessage(tm.conn, kind, string(contents))
}

func (tm *tlvMessager) SendS2CResults(throughputKbps, unsentBytes, totalSentBytes int64) error {
	msg := fmt.Sprintf("%d %d %d", throughputKbps, unsentBytes, totalSentBytes)
	return WriteTLVMessage(tm.conn, TestMsg, msg)
}

func (tm *tlvMessager) ReceiveMessage(kind MessageType) ([]byte, error) {
	b, _, err := ReadTLVMessage(tm.conn, kind)
	return b, err
}

func (tm *tlvMessager) Encoding() Encoding {
	return TLV
}

// SendMetrics sends all the required properties out along the NDT control channel.
func SendMetrics(metrics interface{}, m Messager, prefix string) error {
	v := reflect.ValueOf(metrics)
	t := v.Type()
	// Dereference all passed-in pointers
	for t.Kind() == reflect.Ptr {
		v = v.Elem()
		t = v.Type()
	}
	for i := 0; i < v.NumField(); i++ {
		name := t.Field(i).Name
		switch t.Field(i).Type.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			msg := fmt.Sprintf("%s%s: %v\n", prefix, name, v.Field(i).Interface())
			err := m.SendMessage(TestMsg, []byte(msg))
			if err != nil {
				return err
			}
		case reflect.String:
			msg := fmt.Sprintf("%s%s: %s\n", prefix, name, v.Field(i).String())
			err := m.SendMessage(TestMsg, []byte(msg))
			if err != nil {
				return err
			}
		case reflect.Struct:
			data := v.Field(i).Interface()
			var err error
			if s, ok := data.(fmt.Stringer); ok {
				msg := fmt.Sprintf("%s%s: %s\n", prefix, name, s.String())
				err = m.SendMessage(TestMsg, []byte(msg))
			} else {
				err = SendMetrics(v.Field(i).Interface(), m, prefix+name+".")
			}
			if err != nil {
				return err
			}
		default:
			log.Println("Unhandled case in SendMetrics:", t.Field(i).Type.Kind())
		}
	}
	return nil
}
