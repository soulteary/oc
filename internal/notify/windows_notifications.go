// Copyright (c) 2014-2020 The Notify Authors. All rights reserved.
// Use of this source code is governed by the MIT license in LICENSE.

package notify

import (
	"encoding/binary"
	"fmt"
	"unicode/utf16"
)

type windowsNotification struct {
	action uint32
	name   string
}

// FILE_NOTIFY_INFORMATION has a variable-length UTF-16 name. Decode only
// bytes supplied by ReadDirectoryChangesW, without casting to a maximal array
// that extends beyond the allocation (and panics under Go's checkptr).
func decodeWindowsNotifications(data []byte) ([]windowsNotification, error) {
	var records []windowsNotification
	for len(data) != 0 {
		if len(data) < 12 {
			return nil, fmt.Errorf("truncated notification header")
		}
		next := binary.LittleEndian.Uint32(data)
		action := binary.LittleEndian.Uint32(data[4:])
		length := binary.LittleEndian.Uint32(data[8:])
		if action < 1 || action > 5 {
			return nil, fmt.Errorf("invalid notification action")
		}
		if length == 0 || length%2 != 0 || uint64(length) > uint64(len(data)-12) {
			return nil, fmt.Errorf("invalid notification filename length")
		}
		if next != 0 && (uint64(next) < 12+uint64(length) || uint64(next) >= uint64(len(data)) || next%4 != 0) {
			return nil, fmt.Errorf("invalid notification offset")
		}
		name := make([]uint16, int(length)/2)
		for i := range name {
			name[i] = binary.LittleEndian.Uint16(data[12+2*i:])
		}
		records = append(records, windowsNotification{action: action, name: string(utf16.Decode(name))})
		if next == 0 {
			break
		}
		data = data[next:]
	}
	return records, nil
}
