package imageprep

import (
	"bytes"
	"encoding/binary"
)

const exifOrientationTag = 0x0112

// readOrientation returns the EXIF orientation (1–8) embedded in a JPEG, PNG,
// or WebP source, or 1 when none is present or the metadata is malformed.
func readOrientation(data []byte, mediaType string) int {
	var tiff []byte
	switch mediaType {
	case JPEG:
		tiff = jpegEXIF(data)
	case PNG:
		tiff = pngEXIF(data)
	case WebP:
		tiff = webpEXIF(data)
	}
	if orientation := tiffOrientation(tiff); orientation >= 1 && orientation <= 8 {
		return orientation
	}
	return 1
}

var exifHeader = []byte("Exif\x00\x00")

// jpegEXIF returns the TIFF payload of the first APP1 Exif segment.
func jpegEXIF(data []byte) []byte {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return nil
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return nil
		}
		marker := data[i+1]
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		if marker == 0xDA || marker == 0xD9 { // start of scan or end of image
			return nil
		}
		length := int(binary.BigEndian.Uint16(data[i+2:]))
		if length < 2 || i+2+length > len(data) {
			return nil
		}
		segment := data[i+4 : i+2+length]
		if marker == 0xE1 && bytes.HasPrefix(segment, exifHeader) {
			return segment[len(exifHeader):]
		}
		i += 2 + length
	}
	return nil
}

// pngEXIF returns the payload of the eXIf chunk that precedes image data.
func pngEXIF(data []byte) []byte {
	const signature = "\x89PNG\r\n\x1a\n"
	if !bytes.HasPrefix(data, []byte(signature)) {
		return nil
	}
	for i := len(signature); i+8 <= len(data); {
		length := int(binary.BigEndian.Uint32(data[i:]))
		kind := string(data[i+4 : i+8])
		if length < 0 || i+12+length > len(data) {
			return nil
		}
		switch kind {
		case "eXIf":
			return data[i+8 : i+8+length]
		case "IDAT", "IEND":
			return nil
		}
		i += 12 + length
	}
	return nil
}

// webpEXIF returns the payload of the RIFF EXIF chunk.
func webpEXIF(data []byte) []byte {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return nil
	}
	for i := 12; i+8 <= len(data); {
		kind := string(data[i : i+4])
		length := int(binary.LittleEndian.Uint32(data[i+4:]))
		if length < 0 || i+8+length > len(data) {
			return nil
		}
		if kind == "EXIF" {
			return bytes.TrimPrefix(data[i+8:i+8+length], exifHeader)
		}
		i += 8 + length + length%2
	}
	return nil
}

// tiffOrientation reads the orientation tag from IFD0 of a TIFF structure.
func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 0
	}
	var order binary.ByteOrder
	switch string(tiff[0:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	if order.Uint16(tiff[2:]) != 42 {
		return 0
	}
	offset := int(order.Uint32(tiff[4:]))
	if offset < 8 || offset+2 > len(tiff) {
		return 0
	}
	count := int(order.Uint16(tiff[offset:]))
	for entry := range count {
		start := offset + 2 + entry*12
		if start+12 > len(tiff) {
			return 0
		}
		if order.Uint16(tiff[start:]) != exifOrientationTag {
			continue
		}
		const typeShort = 3
		if order.Uint16(tiff[start+2:]) != typeShort || order.Uint32(tiff[start+4:]) != 1 {
			return 0
		}
		return int(order.Uint16(tiff[start+8:]))
	}
	return 0
}
