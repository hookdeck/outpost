package rsmq

import (
	"math/rand"
	"strconv"
	"strings"
)

func convertStringToInt[n int | int64](s any) n {
	r, err := strconv.ParseInt(s.(string), 10, 0)
	if err != nil {
		panic(err)
	}
	return n(r)
}

func convertStringToUint[n uint | uint64](s any) n {
	r, err := strconv.ParseUint(s.(string), 10, 0)
	if err != nil {
		panic(err)
	}
	return n(r)
}

func convertIntToUint(s any) uint64 {
	return uint64(s.(int64))
}

func convertStringToUnsignedOrDefault[n uint | uint64](s any, def n) n {
	if s == nil {
		return def
	}
	r, err := strconv.ParseUint(s.(string), 10, 0)
	if err != nil {
		panic(err)
	}
	return n(r)
}

const idLetters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

func makeID(n int) string {
	sb := strings.Builder{}
	ln := len(idLetters)
	for i := 0; i < n; i++ {
		sb.WriteByte(idLetters[rand.Int()%ln])
	}
	return sb.String()
}
