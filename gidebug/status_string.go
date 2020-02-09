// Code generated by "stringer -type=Status"; DO NOT EDIT.

package gidebug

import (
	"errors"
	"strconv"
)

var _ = errors.New("dummy error")

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[NotInit-0]
	_ = x[Error-1]
	_ = x[Building-2]
	_ = x[Ready-3]
	_ = x[Running-4]
	_ = x[Stopped-5]
	_ = x[Finished-6]
	_ = x[StatusN-7]
}

const _Status_name = "NotInitErrorBuildingReadyRunningStoppedFinishedStatusN"

var _Status_index = [...]uint8{0, 7, 12, 20, 25, 32, 39, 47, 54}

func (i Status) String() string {
	if i < 0 || i >= Status(len(_Status_index)-1) {
		return "Status(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _Status_name[_Status_index[i]:_Status_index[i+1]]
}

func (i *Status) FromString(s string) error {
	for j := 0; j < len(_Status_index)-1; j++ {
		if s == _Status_name[_Status_index[j]:_Status_index[j+1]] {
			*i = Status(j)
			return nil
		}
	}
	return errors.New("String: " + s + " is not a valid option for type: Status")
}