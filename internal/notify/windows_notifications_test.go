package notify

import (
	"encoding/binary"
	"reflect"
	"testing"
	"unicode/utf16"
)

func notificationRecord(action uint32, name string, more bool) []byte {
	units := utf16.Encode([]rune(name))
	size := 12 + len(units)*2
	if more {
		size = (size + 3) &^ 3
	}
	data := make([]byte, size)
	if more {
		binary.LittleEndian.PutUint32(data, uint32(size))
	}
	binary.LittleEndian.PutUint32(data[4:], action)
	binary.LittleEndian.PutUint32(data[8:], uint32(len(units)*2))
	for i, unit := range units {
		binary.LittleEndian.PutUint16(data[12+2*i:], unit)
	}
	return data
}

func TestDecodeWindowsNotifications(t *testing.T) {
	data := append(notificationRecord(1, "中文😀.txt", true), notificationRecord(2, "a", false)...)
	got, err := decodeWindowsNotifications(data)
	want := []windowsNotification{{1, "中文😀.txt"}, {2, "a"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded %v, %v; want %v", got, err, want)
	}
	if got, err := decodeWindowsNotifications(nil); err != nil || len(got) != 0 {
		t.Fatalf("empty completion: %v, %v", got, err)
	}
}

func TestDecodeWindowsNotificationsRejectsMalformedRecords(t *testing.T) {
	for _, kind := range []string{"header", "invalid-action", "empty-name", "odd-length", "oversized-name", "offset-overlap", "offset-outside", "offset-unaligned", "next-header"} {
		t.Run(kind, func(t *testing.T) {
			data := notificationRecord(1, "abc", false)
			switch kind {
			case "header":
				data = data[:11]
			case "invalid-action":
				binary.LittleEndian.PutUint32(data[4:], 6)
			case "empty-name":
				binary.LittleEndian.PutUint32(data[8:], 0)
			case "odd-length":
				binary.LittleEndian.PutUint32(data[8:], 3)
			case "oversized-name":
				binary.LittleEndian.PutUint32(data[8:], ^uint32(0))
			case "offset-overlap":
				binary.LittleEndian.PutUint32(data, 12)
			case "offset-outside":
				binary.LittleEndian.PutUint32(data, ^uint32(0))
			case "offset-unaligned":
				data = append(data, make([]byte, 20)...)
				binary.LittleEndian.PutUint32(data, 19)
			case "next-header":
				data = append(data, make([]byte, 3)...)
				binary.LittleEndian.PutUint32(data, 20)
			}
			if _, err := decodeWindowsNotifications(data); err == nil {
				t.Fatal("malformed completion was accepted")
			}
		})
	}
}
