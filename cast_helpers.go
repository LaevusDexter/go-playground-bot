package main

import (
	"reflect"
	"unsafe"
)

func add(p unsafe.Pointer, x uintptr) unsafe.Pointer {
	return unsafe.Pointer(uintptr(p) + x)
}

func s2b(str string) (bs []byte) {
	bsoff := unsafe.Offsetof(reflect.SliceHeader{}.Data)
	stroff := unsafe.Offsetof(reflect.StringHeader{}.Data)

	*(*unsafe.Pointer)(add(unsafe.Pointer(&bs), bsoff)) = *(*unsafe.Pointer)(add(unsafe.Pointer(&str), stroff))
	(*reflect.SliceHeader)(unsafe.Pointer(&bs)).Len = len(str)
	(*reflect.SliceHeader)(unsafe.Pointer(&bs)).Cap = len(str)

	return
}

func b2s(bs []byte) (str string) {
	stroff := unsafe.Offsetof(reflect.StringHeader{}.Data)
	bsoff := unsafe.Offsetof(reflect.SliceHeader{}.Data)

	*(*unsafe.Pointer)(add(unsafe.Pointer(&str), stroff)) = *(*unsafe.Pointer)(add(unsafe.Pointer(&bs), bsoff))
	(*reflect.StringHeader)(unsafe.Pointer(&str)).Len = len(bs)

	return
}

func p2b(p unsafe.Pointer) (bs []byte) {
	bsoff := unsafe.Offsetof(reflect.SliceHeader{}.Data)

	*(*unsafe.Pointer)(add(unsafe.Pointer(&bs), bsoff)) = p

	return
}

var efacePtrOff uintptr

func i2p(p interface{}) unsafe.Pointer {
	return *(*unsafe.Pointer)(add(unsafe.Pointer(&p), efacePtrOff))
}

func init() {
	f, ok := reflect.ValueOf(reflect.Value{}).Type().FieldByName("ptr")
	if !ok {
		panic("unable to find the ptr")
	}

	efacePtrOff = f.Offset
}
