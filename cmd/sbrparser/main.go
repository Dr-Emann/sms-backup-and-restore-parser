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
	"encoding/xml"
	"flag"
	"fmt"
	"github.com/danzek/sms-backup-and-restore-parser/smsbackuprestore"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type StreamingOutput struct {
	mmsOut   *smsbackuprestore.MMSOutput
	smsOut   *smsbackuprestore.SMSOutput
	imageDir string

	smsCount                     int
	mmsCount                     int
	numImagesIdentified          int
	numImagesSuccessfullyWritten int
	imageOutputErrors            []error

	closeFuncs []func() error
}

func NewStreamingOutput(outputDir string) (*StreamingOutput, error) {
	imageDir := filepath.Join(outputDir, "images")
	err := os.MkdirAll(imageDir, os.ModePerm)
	if err != nil {
		return nil, fmt.Errorf("unable to create image directory %s: %w", imageDir, err)
	}

	var closeFuncs []func() error
	defer func() {
		for _, closeFunc := range closeFuncs {
			_ = closeFunc()
		}
	}()
	mmsFile, err := os.Create(filepath.Join(outputDir, "mms.tsv"))
	if err != nil {
		return nil, fmt.Errorf("unable to create file mms.tsv: %w", err)
	}
	closeFuncs = append(closeFuncs, mmsFile.Close)
	mmsBufFile := bufio.NewWriter(mmsFile)
	closeFuncs = append(closeFuncs, mmsBufFile.Flush)

	mmsOut, err := smsbackuprestore.NewMMSOutput(mmsBufFile)
	if err != nil {
		return nil, err
	}

	smsFile, err := os.Create(filepath.Join(outputDir, "sms.tsv"))
	if err != nil {
		return nil, fmt.Errorf("unable to create file sms.tsv: %w", err)
	}
	closeFuncs = append(closeFuncs, smsFile.Close)
	smsBufFile := bufio.NewWriter(smsFile)
	closeFuncs = append(closeFuncs, smsBufFile.Flush)

	smsOut, err := smsbackuprestore.NewSMSOutput(smsBufFile)
	if err != nil {
		return nil, err
	}

	result := &StreamingOutput{
		mmsOut:     mmsOut,
		smsOut:     smsOut,
		imageDir:   imageDir,
		closeFuncs: closeFuncs,
	}
	// clear closeFuncs so that they are not called in the defer
	closeFuncs = nil
	result.mmsOut.WithImage = func(fileName string, data []byte) error {
		result.numImagesIdentified++
		fullFilePath := filepath.Join(result.imageDir, fileName)
		err := os.WriteFile(fullFilePath, data, 0o644)
		if err != nil {
			result.imageOutputErrors = append(result.imageOutputErrors, err)
		} else {
			result.numImagesSuccessfullyWritten++
		}
		return nil
	}
	return result, nil
}

func (s *StreamingOutput) ReadFile(file io.Reader) error {
	decoder, err := s.Decoder(file)
	if err != nil {
		return err
	}
	return decoder.Decode()
}

func (s *StreamingOutput) Decoder(file io.Reader) (*smsbackuprestore.MessageDecoder, error) {
	decoder, err := smsbackuprestore.NewMessageDecoder(file)
	if err != nil {
		return nil, err
	}
	decoder.OnSMS = s.onSms
	decoder.OnMMS = s.onMms

	return decoder, nil
}

func (s *StreamingOutput) Close() {
	for _, closeFunc := range s.closeFuncs {
		_ = closeFunc()
	}
}

func (s *StreamingOutput) onSms(sms *smsbackuprestore.SMS) error {
	s.smsCount++
	return s.smsOut.Write(sms)
}

func (s *StreamingOutput) onMms(mms *smsbackuprestore.MMS) error {
	s.mmsCount++
	return s.mmsOut.Write(mms)
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

	streamingOut, err := NewStreamingOutput(*pOutputDirectory)
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
		err = handleFile(err, xmlFilePath, *pOutputDirectory, streamingOut)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error handling file: %q\n", err)
		}
	}

	fmt.Println("Finished generating SMS output")
	fmt.Println("sms.tsv file contains tab-separated values (TSV), i.e. use tab character as the delimiter")
	fmt.Println("Finished generating MMS output")
	fmt.Println("mms.tsv file contains tab-separated values (TSV), i.e. use tab character as the delimiter")
	// print completion messages
	fmt.Printf("\nCompleted in %.2f seconds.\n", time.Since(start).Seconds())
	fmt.Printf("Output saved to %s\n", *pOutputDirectory)
}

func handleFile(err error, xmlFilePath string, outputDir string, out *StreamingOutput) error {
	// get just file name and perform verification checks (assumes default lowercase naming convention)
	fileName := filepath.Base(xmlFilePath)
	if !(strings.HasPrefix(fileName, "calls-") || strings.HasPrefix(fileName, "sms-")) || filepath.Ext(fileName) != ".xml" {
		return fmt.Errorf("unexpected file name: %s", fileName)
	}
	f, err := os.Open(xmlFilePath)
	if err != nil {
		return fmt.Errorf("error opening '%s': %w", xmlFilePath, err)
	}
	defer f.Close()
	bufReader := bufio.NewReaderSize(f, 1024*1024)

	// determine file type
	if strings.HasPrefix(fileName, "sms-") {
		decoder, err := out.Decoder(bufReader)
		if err != nil {
			return err
		}
		startSMSCount := out.smsCount
		startMMSCount := out.mmsCount
		fmt.Printf("Reading %v messages from %v\n", decoder.Messages.Count, fileName)
		if err = decoder.Decode(); err != nil {
			return err
		}
		lengthSMS := out.smsCount - startSMSCount
		lengthMMS := out.mmsCount - startMMSCount

		fmt.Println("\nXML File Validation / QC")
		fmt.Println("===============================================================")
		fmt.Printf("Backup Date: %s\n", decoder.Messages.BackupDate.String())
		fmt.Printf("Message count reported by SMS Backup and Restore app: %s\n", decoder.Messages.Count)

		// convert reportedCount to int for later comparison/validation
		count, err := strconv.Atoi(decoder.Messages.Count)
		if err != nil {
			fmt.Printf("Error converting reported count to integer: %s", decoder.Messages.Count)
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
	} else {
		decoder := xml.NewDecoder(bufReader)
		// calls backup
		// instantiate calls object
		c := new(smsbackuprestore.Calls)
		if err := decoder.Decode(c); err != nil {
			panic(err)
		}

		// print validation / qc / stats to stdout
		c.PrintCallCountQC()

		// generate calls output
		CallsOutput(c, outputDir)
	}
	return nil
}
