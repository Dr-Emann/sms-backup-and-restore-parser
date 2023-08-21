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

// Package main for command-line SMS Backup & Restore parser.
// This tool parses SMS Backup & Restore Android app XML output.
package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/danzek/sms-backup-and-restore-parser/smsbackuprestore"
	_ "github.com/mattn/go-sqlite3"
	"github.com/schollz/progressbar/v3"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type StreamingOutput struct {
	db            *sql.DB
	tx            *sql.Tx
	insertSMS     *sql.Stmt
	insertMMS     *sql.Stmt
	insertMMSPart *sql.Stmt
	smsCount      int
	mmsCount      int
	callCount     int
}

func NewStreamingOutput(ctx context.Context, outputDir string) (*StreamingOutput, error) {
	db, err := sql.Open("sqlite3", filepath.Join(outputDir, "result.db"))
	if err != nil {
		return nil, err
	}

	query := `
		CREATE TABLE IF NOT EXISTS sms (
			id integer primary key autoincrement,
			protocol text,
			address text,
			ty text,
			subject text,
			body text,
			service_center text,
			status integer,
			read integer,
			date long,
			locked boolean,
			date_sent long,
			readable_date text,
			contact_name text
	    );
	    CREATE TABLE IF NOT EXISTS mms (
			id integer primary key autoincrement,
			text_only boolean,
			read integer,
			date long,
			locked boolean,
			date_sent long,
			readable_date text,
			contact_name text,
			seen boolean,
			from_address text,
			address text,
			message_classifier text,
			message_size text,
			addresses_joined text
		);
		CREATE TABLE IF NOT EXISTS mms_parts (
			id integer primary key autoincrement,
			mms_id integer references mms(id),
			content_type text,
			name text,
			file_name text,
			content_display text,
			text text,
			raw_data blob
		);
    `
	_, err = db.Exec(query)
	if err != nil {
		return nil, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	smsStmt, err := tx.Prepare(`
		INSERT INTO sms (protocol, address, ty, subject, body, service_center, status, read, date, locked, date_sent, readable_date, contact_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return nil, err
	}
	mmsStmt, err := tx.Prepare(`
		INSERT INTO MMS (text_only, read, date, locked, date_sent, readable_date, contact_name, seen, from_address, address, message_classifier, message_size, addresses_joined)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return nil, err
	}
	partStmt, err := tx.Prepare(`
		INSERT INTO MMS_PARTS (mms_id, content_type, name, file_name, content_display, text, raw_data)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)

	return &StreamingOutput{
		db:        db,
		tx:        tx,
		insertSMS: smsStmt, insertMMS: mmsStmt, insertMMSPart: partStmt,
	}, nil
}

func (s *StreamingOutput) MessageDecoder(file io.Reader) (*smsbackuprestore.MessageDecoder, error) {
	decoder, err := smsbackuprestore.NewMessageDecoder(file)
	if err != nil {
		return nil, err
	}
	expectedLen, parseErr := strconv.ParseInt(decoder.BackupInfo.Count, 10, 64)
	if parseErr != nil {
		expectedLen = -1
	}
	pb := progressbar.Default(expectedLen, "messages")
	progressbar.OptionSetItsString("msg")(pb)
	decoder.OnSMS = func(sms *smsbackuprestore.SMS) error {
		pb.Add(1)
		return s.onSms(sms)
	}
	decoder.OnMMS = func(mms *smsbackuprestore.MMS) error {
		pb.Add(1)
		return s.onMMS(mms)
	}
	return decoder, nil
}

func (s *StreamingOutput) Commit() error {
	return s.tx.Commit()
}

func (s *StreamingOutput) Close() {
	s.insertSMS.Close()
	s.insertMMS.Close()
	s.insertMMSPart.Close()
	s.tx.Rollback()
	s.db.Close()
}

func (s *StreamingOutput) onSms(sms *smsbackuprestore.SMS) error {
	s.smsCount++
	_, err := s.insertSMS.Exec(
		sms.Protocol,
		sms.Address.String(),
		sms.Type.String(),
		strOrNil(sms.Subject),
		sms.Body,
		strOrNil(sms.ServiceCenter.String()),
		sms.Status.String(),
		sms.Read.String(),
		sms.Date,
		sms.Locked,
		sms.DateSent,
		sms.ReadableDate,
		sms.ContactName,
	)
	return err
}

func (s *StreamingOutput) onMMS(mms *smsbackuprestore.MMS) error {
	s.mmsCount++

	type AddressInfo struct {
		Address    string `json:"address"`
		RawAddress string `json:"raw_address"`
		Type       string `json:"type"`
		Charset    string `json:"charset"`
	}
	addresses := make([]AddressInfo, len(mms.Addresses))
	for i, address := range mms.Addresses {
		addresses[i] = AddressInfo{
			Address:    address.Address.String(),
			RawAddress: string(address.Address),
			Type:       address.Type.String(),
			Charset:    address.Charset,
		}
	}
	addressesJoined, err := json.Marshal(addresses)
	if err != nil {
		return err
	}
	res, err := s.insertMMS.Exec(
		mms.TextOnly,
		mms.Read.String(),
		mms.Date,
		mms.Locked,
		mms.DateSent,
		mms.ReadableDate,
		mms.ContactName,
		mms.Seen,
		strOrNil(mms.FromAddress.String()),
		mms.Address.String(),
		strOrNil(mms.MessageClassifier),
		strOrNil(mms.MessageSize),
		addressesJoined,
	)
	if err != nil {
		return err
	}
	mmsID, err := res.LastInsertId()
	if err != nil {
		return err
	}

	for _, part := range mms.Parts {
		var rawData []byte
		if part.Base64Data != "" {
			rawData, err = base64.StdEncoding.DecodeString(part.Base64Data)
			if err != nil {
				return fmt.Errorf("error decoding base64 data: %w", err)
			}
		}
		_, err = s.insertMMSPart.Exec(
			mmsID,
			strOrNil(part.ContentType),
			strOrNil(part.Name),
			strOrNil(part.FileName),
			strOrNil(part.ContentDisplay),
			strOrNil(part.Text),
			rawData,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func strOrNil(s string) *string {
	if s == "" || s == "null" {
		return nil
	}
	return &s
}

// GetExecutablePath returns the absolute path to the location where this executable is being ran from
func GetExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return ".", fmt.Errorf("Error: Try running this application from another location: %q\n", err)
	}

	exePath, err := filepath.Abs(filepath.Dir(exe))
	if err != nil {
		return ".", fmt.Errorf("Error: Try running this application from another location: %q\n", err)
	}

	return exePath, nil
}

// main function for command-line SMS Backup & Restore app XML output parser.
func main() {
	// time program execution
	start := time.Now()

	// get executable path
	exePath, err := GetExecutablePath()
	if err != nil {
		panic(err)
	}

	// parse command-line args/flags
	pOutputDirectory := flag.String("d", exePath, "Directory path for parsed output (current executable directory is default)")
	flag.Parse()

	// validate output directory
	if outputDirInfo, err := os.Stat(*pOutputDirectory); os.IsNotExist(err) || !outputDirInfo.IsDir() {
		fmt.Fprintf(os.Stderr, "Invalid output directory path: %s", *pOutputDirectory)
		return
	}
	fmt.Printf("Output directory set to %s\n", *pOutputDirectory)

	if len(flag.Args()) <= 0 {
		fmt.Fprint(os.Stderr, "Missing required argument: Specify path to xml backup file(s).\n"+
			"Example: sbrparser.exe C:\\Users\\4n68r\\Documents\\sms-20180213135542.xml\n") // todo -- use name of executable
		return
	}

	streamingOut, err := NewStreamingOutput(context.Background(), *pOutputDirectory)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output files: %q\n", err)
	}
	defer streamingOut.Close()
	for _, xmlFilePath := range flag.Args() {
		// ensure file is valid (file path to xml file with sms backup and restore output)
		fileInfo, err := os.Stat(xmlFilePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error with path to XML file: %q\n", err)
			return
		} else if fileInfo.IsDir() {
			fmt.Fprint(os.Stderr, "XML path must point to specific XML filename, not to a directory.\n")
			return
		}

		// open xml file
		err = handleFile(err, xmlFilePath, streamingOut)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error handling file: %q\n", err)
		}
	}

	err = streamingOut.Commit()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error committing transaction: %q\n", err)
	}

	if streamingOut.smsCount > 0 {
		fmt.Printf("%-10d SMS messages processed\n", streamingOut.smsCount)
	}
	if streamingOut.mmsCount > 0 {
		fmt.Printf("%-10d MMS messages processed\n", streamingOut.mmsCount)
	}
	if streamingOut.callCount > 0 {
		fmt.Printf("%-10d calls processed\n", streamingOut.callCount)
	}
	// print completion messages
	fmt.Printf("\nCompleted in %.2f seconds.\n", time.Since(start).Seconds())
	fmt.Printf("Output saved to %s\n", *pOutputDirectory)
}

func handleFile(err error, xmlFilePath string, out *StreamingOutput) error {
	// get just file name and perform verification checks (assumes default lowercase naming convention)
	fileName := filepath.Base(xmlFilePath)
	if !(strings.HasPrefix(fileName, "calls") || strings.HasPrefix(fileName, "sms")) ||
		(filepath.Ext(fileName) != ".xml" && filepath.Ext(fileName) != ".zip") {
		return fmt.Errorf("unexpected file name: %s", fileName)
	}
	f, err := smsbackuprestore.OpenBackup(xmlFilePath)
	if err != nil {
		return err
	}
	defer f.Close()

	bufReader := bufio.NewReaderSize(f, 1024*1024)

	// determine file type
	if strings.HasPrefix(fileName, "sms") {
		decoder, err := out.MessageDecoder(bufReader)
		if err != nil {
			return err
		}
		startSMSCount := out.smsCount
		startMMSCount := out.mmsCount
		if err = decoder.Decode(); err != nil {
			return err
		}
		lengthSMS := out.smsCount - startSMSCount
		lengthMMS := out.mmsCount - startMMSCount

		fmt.Println("\nXML File Validation / QC")
		fmt.Println("===============================================================")
		fmt.Printf("Backup Date: %s\n", decoder.BackupInfo.BackupDate.String())
		fmt.Printf("Message count reported by SMS Backup and Restore app: %s\n", decoder.BackupInfo.Count)

		// convert reportedCount to int for later comparison/validation
		count, err := strconv.Atoi(decoder.BackupInfo.Count)
		if err != nil {
			fmt.Printf("Error converting reported count to integer: %s", decoder.BackupInfo.Count)
			count = 0
		}

		fmt.Printf("Actual # SMS messages identified: %d\n", lengthSMS)
		fmt.Printf("Actual # MMS messages identified: %d\n", lengthMMS)
		fmt.Printf("Total actual messages identified: %d ... ", lengthSMS+lengthMMS)
		if lengthSMS+lengthMMS == count {
			fmt.Print("OK\n")
		} else {
			fmt.Print("DISCREPANCY DETECTED\n")
		}
		fmt.Println("Finished generating SMS/MMS output")
	} else {
		// todo -- handle call logs
	}
	return nil
}
