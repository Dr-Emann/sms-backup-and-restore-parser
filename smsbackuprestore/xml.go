package smsbackuprestore

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

type MessageDecoder struct {
	decoder *xml.Decoder

	Messages Messages

	// By default, the decoder will use this to populate the SMS and MMS slices in Messages.
	OnSMS func(*SMS) error
	OnMMS func(*MMS) error
}

func NewMessageDecoder(stream io.Reader) (*MessageDecoder, error) {
	decoder := xml.NewDecoder(stream)

	var m Messages
	root, err := findElem(decoder, "smses")
	if err != nil {
		return nil, fmt.Errorf("unable to find root smses element: %w", err)
	}
	for _, attr := range root.Attr {
		if attr.Name.Local == "count" {
			m.Count = attr.Value
		} else if attr.Name.Local == "backup_set" {
			m.BackupSet = attr.Value
		} else if attr.Name.Local == "backup_date" {
			m.BackupDate = AndroidTS(attr.Value)
		}
	}
	result := &MessageDecoder{
		decoder:  decoder,
		Messages: m,
	}
	result.OnSMS = func(sms *SMS) error {
		result.Messages.SMS = append(result.Messages.SMS, *sms)
		return nil
	}
	result.OnMMS = func(mms *MMS) error {
		result.Messages.MMS = append(result.Messages.MMS, *mms)
		return nil
	}
	return result, nil
}

func (d *MessageDecoder) Decode() error {
	for {
		child, err := findElem(d.decoder, "sms", "mms")
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("unable to find sms or mms element: %w", err)
		}

		if child.Name.Local == "sms" {
			var sms SMS
			if err = d.decoder.DecodeElement(&sms, &child); err != nil {
				return fmt.Errorf("unable to decode sms element: %w", err)
			}
			if err = d.OnSMS(&sms); err != nil {
				return err
			}
		} else if child.Name.Local == "mms" {
			var mms MMS
			if err = d.decoder.DecodeElement(&mms, &child); err != nil {
				return fmt.Errorf("unable to decode mms element: %w", err)
			}
			if err = d.OnMMS(&mms); err != nil {
				return err
			}
		} else {
			panic("unexpected element")
		}
	}
}

func findElem(decoder *xml.Decoder, names ...string) (xml.StartElement, error) {
	namesSet := make(map[string]struct{}, len(names))
	for _, name := range names {
		namesSet[name] = struct{}{}
	}
	for {
		t, err := decoder.Token()
		if err != nil {
			return xml.StartElement{}, err
		}
		switch se := t.(type) {
		case xml.StartElement:
			if _, ok := namesSet[se.Name.Local]; ok {
				return se, nil
			} else {
				err := decoder.Skip()
				if err != nil {
					return xml.StartElement{}, err
				}
			}
		}
	}
}
