package smsbackuprestore

import (
	"log"
	"strings"
)

type Contact struct {
	Name            string
	CanonicalNumber string
	RawNumbers      []string
}

func (c *Contact) addRawNum(rawNum string) {
	for _, num := range c.RawNumbers {
		if num == rawNum {
			return
		}
	}
	c.RawNumbers = append(c.RawNumbers, rawNum)
}

// GuessContacts attempts to guess which contacts are associated with which phone numbers
//
// It returns a map from canonical phone number to Contact
func (m *Messages) GuessContacts() (map[string]*Contact, error) {
	var canonicalMap = make(map[string]*Contact)

	for _, sms := range m.SMS {
		// SMS is always to a single contact
		rawNum := string(sms.Address)
		canonicalNum := NormalizePhoneNumber(rawNum)
		if contact, ok := canonicalMap[canonicalNum]; ok {
			if contact.Name != sms.ContactName {
				if contact.Name == "(Unknown)" {
					contact.Name = sms.ContactName
				} else if sms.ContactName == "(Unknown)" {
					// do nothing, the existing name is better
				} else {
					log.Printf("Warning: %s has multiple names: %s and %s", canonicalNum, contact.Name, sms.ContactName)
				}
			}
			contact.addRawNum(rawNum)
		} else {
			canonicalMap[canonicalNum] = &Contact{
				Name:            sms.ContactName,
				CanonicalNumber: canonicalNum,
				RawNumbers:      []string{rawNum},
			}
		}
	}

	// ownNum := ""
	for _, mms := range m.MMS {
		rawNumStr := string(mms.Address)
		rawNumsList := strings.Split(rawNumStr, "~")
		contactNames := strings.Split(RemoveCommasBeforeSuffixes(mms.ContactName), ",")
		for i := range contactNames {
			contactNames[i] = strings.TrimSpace(contactNames[i])
		}
		if len(rawNumsList) != len(contactNames) {
			reason := "A contact probably has a comma"
			if len(rawNumsList) > len(contactNames) {
				reason = "A number probably doesn't have a known contact"
			}
			log.Printf("Warning: mms has %d numbers, but %d contact names. %s",
				len(rawNumsList), len(contactNames), reason)
			log.Printf("rawNumsList: %v", rawNumsList)
			log.Printf("contactNames: %v", contactNames)
			if len(mms.Parts) > 1 {
				log.Printf("text: %s", mms.Parts[1].Text)
			}
			continue
		}
	}
	return canonicalMap, nil
}
