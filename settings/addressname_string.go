// Code generated by "stringer -type=AddressName -linecomment"; DO NOT EDIT.

package settings

import "strconv"

const _AddressName_name = "reserveburnerbanknetworkwrapperpricingwhitelistsetrate"

var _AddressName_index = [...]uint8{0, 7, 13, 17, 24, 31, 38, 47, 54}

func (i AddressName) String() string {
	if i < 0 || i >= AddressName(len(_AddressName_index)-1) {
		return "AddressName(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _AddressName_name[_AddressName_index[i]:_AddressName_index[i+1]]
}
