// Code generated by "stringer -type=SymbolsViewScope"; DO NOT EDIT.

package gide

import (
	"errors"
	"strconv"
)

var _ = errors.New("dummy error")

const _SymbolsViewScope_name = "SymScopeFileSymScopePackageSymScopeN"

var _SymbolsViewScope_index = [...]uint8{0, 12, 27, 36}

func (i SymbolsViewScope) String() string {
	if i < 0 || i >= SymbolsViewScope(len(_SymbolsViewScope_index)-1) {
		return "SymbolsViewScope(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _SymbolsViewScope_name[_SymbolsViewScope_index[i]:_SymbolsViewScope_index[i+1]]
}

func (i *SymbolsViewScope) FromString(s string) error {
	for j := 0; j < len(_SymbolsViewScope_index)-1; j++ {
		if s == _SymbolsViewScope_name[_SymbolsViewScope_index[j]:_SymbolsViewScope_index[j+1]] {
			*i = SymbolsViewScope(j)
			return nil
		}
	}
	return errors.New("String: " + s + " is not a valid option for type: SymbolsViewScope")
}
