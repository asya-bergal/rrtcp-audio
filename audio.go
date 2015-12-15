package main

import (
	"encoding/binary"
	"fmt"
	"github.com/gordonklaus/portaudio"
	"io"
	"os"
	"os/signal"
	"strings"
)

var outfile *os.File
var nSamples int

var c commonChunk
var audio io.Reader
var remaining int

func main() {
	fmt.Println("Playing and recording. Press Ctrl-C to stop.")

	if len(os.Args) < 3 {
		fmt.Println("missing required argument")
		return
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, os.Kill)

	inname := os.Args[1]
	outname := os.Args[2]

	if !strings.HasSuffix(outname, ".aiff") {
		outname += ".aiff"
	}
	if !strings.HasSuffix(inname, ".aiff") {
		inname += ".aiff"
	}

	var err error
	outfile, err = os.Create(outname)
	chk(err)

	infile, err := os.Open(inname)
	chk(err)
	defer infile.Close()

	id, data, err := readChunk(infile)
	chk(err)
	if id.String() != "FORM" {
		fmt.Println("bad file format")
		return
	}
	_, err = data.Read(id[:])
	chk(err)
	if id.String() != "AIFF" {
		fmt.Println("bad file format")
		return
	}
	for {
		id, chunk, err := readChunk(data)
		if err == io.EOF {
			break
		}
		chk(err)
		switch id.String() {
		case "COMM":
			chk(binary.Read(chunk, binary.BigEndian, &c))
		case "SSND":
			chunk.Seek(8, 1) //ignore offset and block
			audio = chunk
		default:
			fmt.Printf("ignoring unknown chunk '%s'\n", id)
		}
	}

	// form chunk
	_, err = outfile.WriteString("FORM")
	chk(err)
	chk(binary.Write(outfile, binary.BigEndian, int32(0))) //total bytes
	_, err = outfile.WriteString("AIFF")
	chk(err)

	// common chunk
	_, err = outfile.WriteString("COMM")
	chk(err)
	chk(binary.Write(outfile, binary.BigEndian, int32(18)))                  //size
	chk(binary.Write(outfile, binary.BigEndian, int16(1)))                   //channels
	chk(binary.Write(outfile, binary.BigEndian, int32(0)))                   //number of samples
	chk(binary.Write(outfile, binary.BigEndian, int16(32)))                  //bits per sample
	_, err = outfile.Write([]byte{0x40, 0x0e, 0xac, 0x44, 0, 0, 0, 0, 0, 0}) //80-bit sample rate 44100
	chk(err)

	// sound chunk
	_, err = outfile.WriteString("SSND")
	chk(err)
	chk(binary.Write(outfile, binary.BigEndian, int32(0))) //size
	chk(binary.Write(outfile, binary.BigEndian, int32(0))) //offset
	chk(binary.Write(outfile, binary.BigEndian, int32(0))) //block
	nSamples = 0
	defer func() {
		// fill in missing sizes
		totalBytes := 4 + 8 + 18 + 8 + 8 + 4*nSamples
		_, err = outfile.Seek(4, 0)
		chk(err)
		chk(binary.Write(outfile, binary.BigEndian, int32(totalBytes)))
		_, err = outfile.Seek(22, 0)
		chk(err)
		chk(binary.Write(outfile, binary.BigEndian, int32(nSamples)))
		_, err = outfile.Seek(42, 0)
		chk(err)
		chk(binary.Write(outfile, binary.BigEndian, int32(4*nSamples+8)))
		chk(outfile.Close())
	}()

	portaudio.Initialize()
	defer portaudio.Terminate()
	h, err := portaudio.DefaultHostApi()
	chk(err)

	remaining = int(c.NumSamples)

	p := portaudio.HighLatencyParameters(h.DefaultInputDevice, h.DefaultOutputDevice)
	p.Input.Channels = 1
	p.Output.Channels = 1
	p.SampleRate = 44100
	p.FramesPerBuffer = 64

	stream, err := portaudio.OpenStream(p, processAudio)
	chk(err)

	defer stream.Close()

	chk(stream.Start())
	defer stream.Stop()
	for {
		select {
		case <-sig:
			return
		default:
		}
	}
}

//Called every FramesPerBuffer/SampleRate seconds
func processAudio(in []int32, out []int32) {
	chk(binary.Write(outfile, binary.BigEndian, in))
	nSamples += len(in)

	if len(out) > remaining {
		out = out[:remaining]
	}
	err := binary.Read(audio, binary.BigEndian, out)
	if err == io.EOF {
		return
	}
	chk(err)
	remaining -= len(out)
}

func readChunk(r readerAtSeeker) (id ID, data *io.SectionReader, err error) {
	_, err = r.Read(id[:])
	if err != nil {
		return
	}
	var n int32
	err = binary.Read(r, binary.BigEndian, &n)
	if err != nil {
		return
	}
	off, _ := r.Seek(0, 1)
	data = io.NewSectionReader(r, off, int64(n))
	_, err = r.Seek(int64(n), 1)
	return
}

type readerAtSeeker interface {
	io.Reader
	io.ReaderAt
	io.Seeker
}

type ID [4]byte

func (id ID) String() string {
	return string(id[:])
}

type commonChunk struct {
	NumChans      int16
	NumSamples    int32
	BitsPerSample int16
	SampleRate    [10]byte
}

func chk(err error) {
	if err != nil {
		panic(err)
	}
}
