package main

//go:generate protoc fileformat.proto --go_out=.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/codesoap/zstd-pbf/pbfproto"
	"github.com/klauspost/compress/zlib"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
)

// See https://wiki.openstreetmap.org/wiki/PBF_Format#File_format
const maxBlobHeaderSize = 64 * 1024 * 1024

var compressionLevel = zstd.SpeedDefault
var speedFastest bool
var speedBetterCompression bool
var speedBestCompression bool
var inFile = ""
var outFile = ""
var zstdEncoder *zstd.Encoder
var headerSizeBuf = make([]byte, 4)
var rawBlobBuf []byte

func init() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr,
			"Usage:\n  zstd-pbf [-fastest|-better|-best] <IN_FILE> <OUT_FILE>")
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
	}
	flag.BoolVar(&speedFastest, "fastest", false, "use the fastest compression level")
	flag.BoolVar(&speedBetterCompression, "better", false, "use a compression level with better compression than default")
	flag.BoolVar(&speedBestCompression, "best", false, "use the compression level with the best compression")
	flag.Parse()
	if speedFastest {
		if speedBetterCompression || speedBestCompression {
			fmt.Fprintln(os.Stderr, "Multiple compression levels have been requested.")
			os.Exit(1)
		}
		compressionLevel = zstd.SpeedFastest
	}
	if speedBetterCompression {
		if speedFastest || speedBestCompression {
			fmt.Fprintln(os.Stderr, "Multiple compression levels have been requested.")
			os.Exit(1)
		}
		compressionLevel = zstd.SpeedBetterCompression
	}
	if speedBestCompression {
		if speedFastest || speedBetterCompression {
			fmt.Fprintln(os.Stderr, "Multiple compression levels have been requested.")
			os.Exit(1)
		}
		compressionLevel = zstd.SpeedBestCompression
	}
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr,
			"Give exactly two arguments: The input and output PBF files.")
		os.Exit(1)
	}
	inFile = flag.Arg(0)
	outFile = flag.Arg(1)
	if _, err := os.Stat(outFile); !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "The file '%s' already exists.\n", outFile)
		os.Exit(1)
	}

	var err error
	zstdEncoder, err = zstd.NewWriter(nil, zstd.WithEncoderLevel(compressionLevel))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not create zstd encoder: %v", err)
	}
}

func main() {
	defer zstdEncoder.Close()
	in, err := os.Open(inFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not open file '%s': %v", inFile, err)
		os.Exit(1)
	}
	defer in.Close()
	out, err := os.Create(outFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not open file '%s': %v", outFile, err)
		os.Exit(1)
	}
	defer out.Close()
	success := false
	defer func() {
		if !success {
			os.Remove(outFile)
		}
	}()
	for {
		// 1. Read data:
		blobHeader, err := readBlobHeader(in)
		if err == io.EOF {
			success = true
			break
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "Could not read BlobHeader: %v", err)
			os.Exit(1)
		}
		blob, err := readBlob(blobHeader, in)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not read Blob: %v", err)
			os.Exit(1)
		}

		// 2. Change compression:
		if err = recompressData(blob); err != nil {
			fmt.Fprintf(os.Stderr, "Could not re-compress Blob: %v", err)
			os.Exit(1)
		}
		rawBlob, err := proto.Marshal(blob)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not serialize Blob: %v", err)
			os.Exit(1)
		}
		datasize := int32(len(rawBlob))
		blobHeader.Datasize = &datasize

		// 3. Write data:
		if err = writeBlobHeader(blobHeader, out); err != nil {
			fmt.Fprintf(os.Stderr, "Could not write BlobHeader: %v", err)
			os.Exit(1)
		}
		if _, err = out.Write(rawBlob); err != nil {
			fmt.Fprintf(os.Stderr, "Could not write Blob: %v", err)
			os.Exit(1)
		}
	}
}

func readBlobHeader(in *os.File) (*pbfproto.BlobHeader, error) {
	size, err := getBlobHeaderSize(in)
	if err != nil {
		return nil, err
	}
	rawBlobBuf, err = readAllIntoBuf(io.LimitReader(in, int64(size)), rawBlobBuf)
	if err != nil {
		return nil, fmt.Errorf("could not read BlobHeader: %v", err)
	}
	header := &pbfproto.BlobHeader{}
	return header, proto.Unmarshal(rawBlobBuf, header)
}

func readBlob(header *pbfproto.BlobHeader, in *os.File) (*pbfproto.Blob, error) {
	var err error
	rawBlobBuf, err = readAllIntoBuf(io.LimitReader(in, int64(*header.Datasize)), rawBlobBuf)
	if err != nil {
		return nil, err
	}
	blob := &pbfproto.Blob{}
	return blob, proto.Unmarshal(rawBlobBuf, blob)
}

func recompressData(blob *pbfproto.Blob) error {
	rawData, err := toRawData(blob)
	if err != nil {
		return err
	}
	out := zstdEncoder.EncodeAll(rawData, nil)
	blob.Data = &pbfproto.Blob_ZstdData{ZstdData: out}
	return err
}

func writeBlobHeader(header *pbfproto.BlobHeader, out *os.File) error {
	rawHeader, err := proto.Marshal(header)
	if err != nil {
		return err
	}
	binary.BigEndian.PutUint32(headerSizeBuf, uint32(len(rawHeader)))
	if _, err := out.Write(headerSizeBuf); err != nil {
		return err
	}
	_, err = out.Write(rawHeader)
	return err
}

func getBlobHeaderSize(file *os.File) (uint32, error) {
	if _, err := io.ReadFull(file, headerSizeBuf); err != nil {
		return 0, err
	}
	size := binary.BigEndian.Uint32(headerSizeBuf)
	if size >= maxBlobHeaderSize {
		return 0, fmt.Errorf("blobHeader size %d >= 64KiB", size)
	}
	return size, nil
}

// toRawData extracts the uncompressed data from blob. It only supports
// uncompressed and zlib compressed blobs.
func toRawData(blob *pbfproto.Blob) ([]byte, error) {
	if blob == nil {
		return nil, fmt.Errorf("blob is nil")
	}
	var data []byte
	switch blobData := blob.Data.(type) {
	case *pbfproto.Blob_Raw:
		data = blobData.Raw
	case *pbfproto.Blob_ZlibData:
		reader, err := zlib.NewReader(bytes.NewReader(blobData.ZlibData))
		if err != nil {
			return data, fmt.Errorf("could not decompress zlib blob: %v", err)
		}
		if cap(rawBlobBuf) < int(*blob.RawSize) {
			rawBlobBuf = make([]byte, *blob.RawSize)
		}
		data = rawBlobBuf[:*blob.RawSize]
		if _, err = io.ReadFull(reader, data); err != nil {
			return data, fmt.Errorf("could not decompress zlib blob: %v", err)
		}
	default:
		return data, fmt.Errorf("found unsupported blob format: %T", blob.Data)
	}
	return data, nil
}

func readAllIntoBuf(r io.Reader, b []byte) ([]byte, error) {
	// Code is mostly copied from io.ReadAll.
	b = b[:0]
	for {
		n, err := r.Read(b[len(b):cap(b)])
		b = b[:len(b)+n]
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return b, err
		}

		if len(b) == cap(b) {
			// Add more capacity (let append pick how much).
			b = append(b, 0)[:len(b)]
		}
	}
}
