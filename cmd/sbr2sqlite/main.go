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
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"github.com/danzek/sms-backup-and-restore-parser/smsbackuprestore"
	_ "github.com/mattn/go-sqlite3"
	"log"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"time"
)

func strOrNil(s string) *string {
	if s == "" || s == "null" {
		return nil
	}
	return &s
}

// SMSOutput calls GenerateSMSOutput() and prints status/errors.
func SMSOutput(m *smsbackuprestore.Messages, db *sql.DB) error {
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
	   )
    `
	_, err := db.Exec(query)
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(context.Background(), nil)
	defer tx.Rollback()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`
		INSERT INTO sms (protocol, address, ty, subject, body, service_center, status, read, date, locked, date_sent, readable_date, contact_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, sms := range m.SMS {
		_, err = stmt.Exec(
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
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MMSOutput calls DecodeImages() and GenerateMMSOutput() and prints status/errors.
func MMSOutput(m *smsbackuprestore.Messages, db *sql.DB) error {
	query := `
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
		)
	`
	_, err := db.Exec(query)
	if err != nil {
		return err
	}
	query = `
		CREATE TABLE IF NOT EXISTS mms_parts (
			id integer primary key autoincrement,
			mms_id integer references mms(id),
			content_type text,
			name text,
			file_name text,
			content_display text,
			text text,
			raw_data blob
		)
	`
	_, err = db.Exec(query)
	if err != nil {
		return err
	}
	query = `
		CREATE VIEW IF NOT EXISTS mms_view AS
		SELECT * from mms join mms_parts on mms.id = mms_parts.mms_id
	`
	_, err = db.Exec(query)
	if err != nil {
		return err
	}
	query = `
		CREATE VIEW IF NOT EXISTS wordle_messages AS
		SELECT * from mms_view where text REGEXP 'Wordle \d* \d/\d' and text not like '%“Wordle%'
	`
	_, err = db.Exec(query)
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(context.Background(), nil)
	defer tx.Rollback()
	if err != nil {
		return err
	}
	mainStmt, err := tx.Prepare(`
		INSERT INTO MMS (text_only, read, date, locked, date_sent, readable_date, contact_name, seen, from_address, address, message_classifier, message_size, addresses_joined)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	defer mainStmt.Close()
	if err != nil {
		return err
	}
	partStmt, err := tx.Prepare(`
		INSERT INTO MMS_PARTS (mms_id, content_type, name, file_name, content_display, text, raw_data)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer partStmt.Close()
	for _, mms := range m.MMS {
		// JSON definition
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
		res, err := mainStmt.Exec(
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
			_, err = partStmt.Exec(
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
	}

	return tx.Commit()
}

// CallsOutput calls GenerateCallOutput() and prints status/errors.
func CallsOutput(c *smsbackuprestore.Calls, outputDir string) {
	// generate calls
	fmt.Println("\nCreating calls output...")
	err := smsbackuprestore.GenerateCallOutput(c, outputDir)
	if err != nil {
		fmt.Printf("Error encountered:\n%q\n", err)
	} else {
		fmt.Println("Finished generating calls output")
		fmt.Println("calls.tsv file contains tab-separated values (TSV), i.e. use tab character as the delimiter")
	}
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
	f, err := os.Create("out.prof")
	if err != nil {
		panic(err)
	}
	pprof.StartCPUProfile(f)
	defer pprof.StopCPUProfile()
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

	if len(flag.Args()) > 0 {
		for _, xmlFilePath := range flag.Args() {
			if handleXmlFile(xmlFilePath, *pOutputDirectory) {
				return
			}
		}
	} else {
		fmt.Fprint(os.Stderr, "Missing required argument: Specify path to xml backup file(s).\n"+
			"Example: sbrparser.exe C:\\Users\\4n68r\\Documents\\sms-20180213135542.xml\n") // todo -- use name of executable
		return
	}

	// print completion messages
	fmt.Printf("\nCompleted in %.2f seconds.\n", time.Since(start).Seconds())
	fmt.Printf("Output saved to %s\n", *pOutputDirectory)
}

func handleXmlFile(xmlFilePath string, pOutputDirectory string) bool {
	// ensure file is valid (file path to xml file with sms backup and restore output)
	fileInfo, err := os.Stat(xmlFilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error with path to XML file: %q\n", err)
		return true
	} else if fileInfo.IsDir() {
		fmt.Fprint(os.Stderr, "XML path must point to specific XML filename, not to a directory.\n")
		return true
	}

	// get just file name and perform verification checks (assumes default lowercase naming convention)
	fileName := filepath.Base(xmlFilePath)
	if !(strings.HasPrefix(fileName, "calls-") || strings.HasPrefix(fileName, "sms-")) || filepath.Ext(fileName) != ".xml" {
		fmt.Fprintf(os.Stderr, "Unexpected file name: %s\n", fileName)
		return true
	}
	// status message
	fmt.Printf("\nLoading %s into memory and parsing (this may take a little while) ...\n", xmlFilePath)

	// read entire file into data variable
	data, fileReadErr := os.ReadFile(xmlFilePath)
	if fileReadErr != nil {
		panic(fileReadErr)
	}

	/*
		// remove null bytes encoded as XML entities because the Java developer of SMS Backup & Restore doesn't understand UTF-8 nor XML
		data = bytes.Replace(data, []byte("&#0;"), []byte(""), -1)

		// attempt to render emoji's properly due to SMS Backup & Restore app rendering of emoji's as HTML entitites in decimal (slow)
		re := regexp.MustCompile(`&#(\d{5});&#(\d{5});`)
		data = smsbackuprestore.ReplaceAllBytesSubmatchFunc(re, data, func(groups [][]byte) []byte {
			high, _ := strconv.Atoi(string(groups[2]))
			low, _ := strconv.Atoi(string(groups[1]))

			return []byte(fmt.Sprintf("&#%d;", int(utf16.Decode([]uint16{uint16(low), uint16(high)})[0])))
		})
	*/

	// determine file type
	if strings.HasPrefix(fileName, "sms-") {
		// sms backup
		// instantiate messages object
		m := new(smsbackuprestore.Messages)
		if err := xml.Unmarshal(data, m); err != nil {
			panic(err)
		}

		// print validation / qc / stats to stdout
		m.PrintMessageCountQC()
		contacts, err := m.GuessContacts()
		if err != nil {
			panic(err)
		}
		fmt.Printf("%#v", contacts)
		return true

		_ = os.Remove("./foo.db")
		db, err := sql.Open("sqlite3", "./foo.db")
		if err != nil {
			log.Fatal(err)
		}
		// generate sms
		err = SMSOutput(m, db)
		if err != nil {
			log.Fatal(err)
		}

		// generate mms
		err = MMSOutput(m, db)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		return false
		// calls backup
		// instantiate calls object
		c := new(smsbackuprestore.Calls)
		if err := xml.Unmarshal(data, c); err != nil {
			panic(err)
		}

		// print validation / qc / stats to stdout
		c.PrintCallCountQC()

		// generate calls output
		CallsOutput(c, pOutputDirectory)
	}
	return false
}
