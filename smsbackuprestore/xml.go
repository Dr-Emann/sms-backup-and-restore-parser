package smsbackuprestore

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

type MessageDecoder struct {
	decoder *xml.Decoder

	BackupInfo BackupInfo

	// Will be called (if non-nil) for each SMS and MMS message found.
	// At least one must be set.
	OnSMS func(*SMS) error
	OnMMS func(*MMS) error
}

func NewMessageDecoder(stream io.Reader) (*MessageDecoder, error) {
	decoder := xml.NewDecoder(stream)

	result := &MessageDecoder{
		decoder: decoder,
	}
	root, err := findElem(decoder, "smses")
	if err != nil {
		return nil, fmt.Errorf("unable to find root smses element: %w", err)
	}
	fillBackupInfo(&result.BackupInfo, root)

	return result, nil
}

func (d *MessageDecoder) Decode() error {
	if d.OnSMS == nil && d.OnMMS == nil {
		panic("OnSMS or OnMMS must be set")
	}
	for {
		child, err := findElem(d.decoder, "sms", "mms")
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("unable to find sms or mms element: %w", err)
		}

		if child.Name.Local == "sms" {
			if d.OnSMS == nil {
				err = d.decoder.Skip()
				if err != nil {
					return err
				}
			} else {
				var sms SMS
				if err = d.decoder.DecodeElement(&sms, &child); err != nil {
					return fmt.Errorf("unable to decode sms element: %w", err)
				}
				if err = d.OnSMS(&sms); err != nil {
					return err
				}
			}
		} else if child.Name.Local == "mms" {
			if d.OnMMS == nil {
				err = d.decoder.Skip()
				if err != nil {
					return err
				}
			} else {
				var mms MMS
				if err = d.decoder.DecodeElement(&mms, &child); err != nil {
					return fmt.Errorf("unable to decode mms element: %w", err)
				}
				if err = d.OnMMS(&mms); err != nil {
					return err
				}
			}
		} else {
			panic("unexpected element")
		}
	}
}

type CallDecoder struct {
	decoder *xml.Decoder

	BackupInfo BackupInfo

	OnCall func(*Call) error
}

func NewCallDecoder(stream io.Reader) (*CallDecoder, error) {
	decoder := xml.NewDecoder(stream)

	result := &CallDecoder{
		decoder: decoder,
	}
	root, err := findElem(decoder, "calls")
	if err != nil {
		return nil, fmt.Errorf("unable to find root calls element: %w", err)
	}
	fillBackupInfo(&result.BackupInfo, root)

	return result, nil
}

func (d *CallDecoder) Decode() error {
	if d.OnCall == nil {
		panic("OnCall must be set")
	}
	for {
		child, err := findElem(d.decoder, "call")
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("unable to find call element: %w", err)
		}

		var call Call
		if err = d.decoder.DecodeElement(&call, &child); err != nil {
			return fmt.Errorf("unable to decode call element: %w", err)
		}
		if err = d.OnCall(&call); err != nil {
			return err
		}
	}
}

func fillBackupInfo(info *BackupInfo, elem xml.StartElement) {
	for _, attr := range elem.Attr {
		if attr.Name.Local == "count" {
			info.Count = attr.Value
		} else if attr.Name.Local == "backup_set" {
			info.BackupSet = attr.Value
		} else if attr.Name.Local == "backup_date" {
			info.BackupDate = AndroidTS(attr.Value)
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
