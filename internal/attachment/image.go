package attachment

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"

	_ "golang.org/x/image/webp"
)

const maxImageHeaderBytes = 1 << 20

// ImageLimits bounds both encoded and decoded image resources.
type ImageLimits struct {
	MaxBytes  int64
	MaxWidth  int
	MaxHeight int
	MaxPixels int64
}

// InspectImage validates the header of a supported raster image and returns a
// PutInput whose final validation checks the complete staged bytes.
func InspectImage(filename string, content io.Reader, limits ImageLimits) (PutInput, error) {
	reader := bufio.NewReader(content)
	header, err := reader.Peek(512)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return PutInput{}, fmt.Errorf("read attachment header: %w", err)
	}
	if len(header) == 0 {
		return PutInput{}, fmt.Errorf("%w: attachment is empty", ErrInvalidInput)
	}
	mediaType := strings.Split(http.DetectContentType(header), ";")[0]
	if mediaType != "image/png" && mediaType != "image/jpeg" && mediaType != "image/gif" && mediaType != "image/webp" {
		return PutInput{}, fmt.Errorf("%w: unsupported attachment type %q", ErrInvalidInput, mediaType)
	}
	var consumed bytes.Buffer
	configuration, format, err := image.DecodeConfig(io.LimitReader(io.TeeReader(reader, &consumed), maxImageHeaderBytes))
	if err != nil || "image/"+format != mediaType {
		return PutInput{}, fmt.Errorf("%w: invalid %s image", ErrInvalidInput, strings.TrimPrefix(mediaType, "image/"))
	}
	pixels := int64(configuration.Width) * int64(configuration.Height)
	if configuration.Width <= 0 || configuration.Height <= 0 ||
		(limits.MaxWidth > 0 && configuration.Width > limits.MaxWidth) ||
		(limits.MaxHeight > 0 && configuration.Height > limits.MaxHeight) ||
		(limits.MaxPixels > 0 && pixels > limits.MaxPixels) {
		return PutInput{}, fmt.Errorf("%w: image dimensions exceed limits", ErrInvalidInput)
	}
	return PutInput{
		Filename: filename, MediaType: mediaType,
		Content:  io.MultiReader(bytes.NewReader(consumed.Bytes()), reader),
		MaxBytes: limits.MaxBytes, Width: configuration.Width, Height: configuration.Height,
		Validate: func(staged io.ReadSeeker) error {
			return validateCompleteImage(staged, format, configuration, limits.MaxPixels)
		},
	}, nil
}

func validateCompleteImage(staged io.Reader, format string, configuration image.Config, maxPixels int64) error {
	data, err := io.ReadAll(staged)
	if err != nil {
		return err
	}
	switch format {
	case "png":
		if !validPNGEnd(data) {
			return errors.New("PNG has data outside IEND")
		}
	case "jpeg":
		if !validJPEGEnd(data) {
			return errors.New("JPEG has data outside EOI")
		}
	case "gif":
		if err := validateGIFBlocks(data, maxPixels); err != nil {
			return err
		}
		if _, err := gif.DecodeAll(bytes.NewReader(data)); err != nil {
			return err
		}
	case "webp":
		if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" || uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
			return errors.New("WebP RIFF length is invalid")
		}
	}
	decoded, decodedFormat, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if decodedFormat != format || decoded.Bounds().Dx() != configuration.Width || decoded.Bounds().Dy() != configuration.Height {
		return errors.New("decoded image does not match its header")
	}
	return nil
}

func validPNGEnd(data []byte) bool {
	if len(data) < 8 || !bytes.Equal(data[:8], []byte("\x89PNG\r\n\x1a\n")) {
		return false
	}
	for offset := 8; offset+12 <= len(data); {
		length := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
		end := uint64(offset) + 12 + length
		if end > uint64(len(data)) {
			return false
		}
		if string(data[offset+4:offset+8]) == "IEND" {
			return length == 0 && end == uint64(len(data))
		}
		offset = int(end)
	}
	return false
}

func validJPEGEnd(data []byte) bool {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return false
	}
	position := 2
	inScan := false
	for position < len(data) {
		if inScan {
			for position < len(data) && data[position] != 0xff {
				position++
			}
			if position >= len(data) {
				return false
			}
		}
		if data[position] != 0xff {
			return false
		}
		for position < len(data) && data[position] == 0xff {
			position++
		}
		if position >= len(data) {
			return false
		}
		marker := data[position]
		position++
		if inScan && (marker == 0x00 || marker >= 0xd0 && marker <= 0xd7) {
			continue
		}
		inScan = false
		switch {
		case marker == 0xd9:
			return position == len(data)
		case marker == 0xd8 || marker >= 0xd0 && marker <= 0xd7 || marker == 0x01:
			continue
		case marker == 0x00 || position+2 > len(data):
			return false
		}
		length := int(binary.BigEndian.Uint16(data[position : position+2]))
		if length < 2 || position+length > len(data) {
			return false
		}
		position += length
		inScan = marker == 0xda
	}
	return false
}

func validateGIFBlocks(data []byte, maxPixels int64) error {
	if len(data) < 13 || (string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a") {
		return errors.New("GIF header is invalid")
	}
	position := 13
	if data[10]&0x80 != 0 {
		position += 3 * (1 << ((data[10] & 0x07) + 1))
	}
	var pixels int64
	skipSubBlocks := func() bool {
		for position < len(data) {
			length := int(data[position])
			position++
			if length == 0 {
				return true
			}
			if position+length > len(data) {
				return false
			}
			position += length
		}
		return false
	}
	for position < len(data) {
		switch data[position] {
		case 0x3b:
			if position+1 != len(data) {
				return errors.New("GIF has data outside its trailer")
			}
			return nil
		case 0x21:
			position += 2
			if position > len(data) || !skipSubBlocks() {
				return errors.New("GIF extension blocks are invalid")
			}
		case 0x2c:
			if position+10 > len(data) {
				return errors.New("GIF image descriptor is truncated")
			}
			width := int64(binary.LittleEndian.Uint16(data[position+5 : position+7]))
			height := int64(binary.LittleEndian.Uint16(data[position+7 : position+9]))
			pixels += width * height
			packed := data[position+9]
			position += 10
			if packed&0x80 != 0 {
				position += 3 * (1 << ((packed & 0x07) + 1))
			}
			if position >= len(data) {
				return errors.New("GIF image data is truncated")
			}
			position++ // LZW minimum code size.
			if !skipSubBlocks() {
				return errors.New("GIF image data is invalid")
			}
			if maxPixels > 0 && pixels > maxPixels {
				return errors.New("GIF cumulative frame pixels exceed limits")
			}
		default:
			return errors.New("GIF block type is invalid")
		}
	}
	return errors.New("GIF does not end at its trailer")
}
