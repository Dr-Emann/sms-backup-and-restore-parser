/*
SBRParser: SMS Backup & Restore Android app parser

Copyright (c) 2018 Dan O'Day <d@4n68r.com>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package smsbackuprestore

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// GenerateSMSOutput outputs a tab-delimited file named "sms.tsv" containing parsed SMS messages from the backup file.
func GenerateSMSOutput(m *Messages, outputDir string) error {
	smsOutput, err := os.Create(filepath.Join(outputDir, "sms.tsv"))
	if err != nil {
		return fmt.Errorf("Unable to create file: sms.tsv\n%q", err)
	}
	defer smsOutput.Close()

	out, err := NewSMSOutput(smsOutput)
	if err != nil {
		return err
	}
	// iterate over sms
	for i := range m.SMS {
		if err := out.Write(&m.SMS[i]); err != nil {
			return err
		}
	}

	return nil
}

type SMSOutput struct {
	f   io.Writer
	idx int
}

func NewSMSOutput(f io.Writer) (*SMSOutput, error) {
	headers := []string{
		"SMS Index #",
		"Protocol",
		"Address",
		"Type",
		"Subject",
		"Body",
		"Service Center",
		"Status",
		"Read",
		"Date",
		"Locked",
		"Date Sent",
		"Readable Date",
		"Contact Name",
	}

	if _, err := fmt.Fprintln(f, strings.Join(headers, "\t")); err != nil {
		return nil, err
	}
	return &SMSOutput{f: f}, nil
}

func (o *SMSOutput) Write(sms *SMS) error {
	row := []string{
		strconv.Itoa(o.idx),
		sms.Protocol,
		sms.Address.String(),
		sms.Type.String(),
		sms.Subject,
		CleanupMessageBody(sms.Body),
		sms.ServiceCenter.String(),
		sms.Status.String(),
		sms.Read.String(),
		sms.Date.String(),
		sms.Locked.String(),
		sms.DateSent.String(),
		sms.ReadableDate,
		RemoveCommasBeforeSuffixes(sms.ContactName),
	}
	o.idx++
	_, err := fmt.Fprintln(o.f, strings.Join(row, "\t"))
	return err
}
