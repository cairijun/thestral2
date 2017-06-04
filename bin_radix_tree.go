package main

import (
	"bytes"
	"fmt"
)

const bitStrWordSize = 32

type bitStr struct {
	Words  []uint32
	BitLen uint
}

func bitStrFromBytes(b []byte, n uint) bitStr {
	if n == 0 {
		return bitStr{}
	}

	nw := (n-1)/bitStrWordSize + 1
	str := bitStr{make([]uint32, nw), n}
	nFullWord := n / bitStrWordSize
	for i := uint(0); i < nFullWord; i++ {
		str.Words[i] = (uint32(b[i*4])<<24 |
			uint32(b[i*4+1])<<16 |
			uint32(b[i*4+2])<<8 |
			uint32(b[i*4+3]))
	}

	nTailingBytes := (n-1)/8 + 1 - nFullWord*4
	if nTailingBytes > 0 {
		lastWord := uint32(b[nFullWord*4]) << 24
		shift := uint(16)
		for i := uint(1); i < nTailingBytes; i++ {
			lastWord |= uint32(b[nFullWord*4+i]) << shift
			shift >>= 1
		}
		str.Words[nw-1] = lastWord
	}

	return str
}

func (s bitStr) Equal(o bitStr) bool {
	if s.BitLen != o.BitLen {
		return false
	}
	n := s.BitLen / bitStrWordSize
	m := s.BitLen % bitStrWordSize
	for i := uint(0); i < n; i++ {
		if s.Words[i] != o.Words[i] {
			return false
		}
	}
	if m != 0 {
		return (s.Words[n]^o.Words[n])>>(bitStrWordSize-m) == 0
	}
	return true
}

func (s bitStr) Bit(i uint) bool {
	w := s.Words[i/bitStrWordSize]
	return 0x1 == ((w >> (bitStrWordSize - (i % bitStrWordSize) - 1)) & 0x1)
}

func (s bitStr) Substr(from, n uint) bitStr {
	if n == 0 {
		return bitStr{}
	}

	nw := (n-1)/bitStrWordSize + 1
	result := bitStr{make([]uint32, nw), n}
	start := from / bitStrWordSize
	shift := from % bitStrWordSize

	if shift == 0 {
		copy(result.Words, s.Words[start:start+nw])
	} else {
		var suffix uint32
		if start+nw < uint(len(s.Words)) {
			suffix = s.Words[start+nw] >> (bitStrWordSize - shift)
		}
		for i := uint(1); i <= nw; i++ {
			curr := s.Words[start+nw-i]
			result.Words[nw-i] = (curr << shift) | suffix
			suffix = curr >> (bitStrWordSize - shift)
		}
	}
	return result
}

func (s bitStr) CommPfxLen(o bitStr) uint {
	n := s.BitLen
	if n > o.BitLen {
		n = o.BitLen
	}
	if n == 0 {
		return 0
	}
	nw := (n-1)/bitStrWordSize + 1

	var l uint
	for i := uint(0); i < nw; i++ {
		l += bitStrWordSize
		m := s.Words[i] ^ o.Words[i]
		if m != 0 {
			for m != 0 {
				m >>= 1
				l--
			}
			break
		}
	}

	if l > n {
		return n
	}
	return l
}

func (s bitStr) Format(f fmt.State, c rune) {
	fmt.Fprintf(f, "[%d]", s.BitLen)
	if s.BitLen > 0 {
		n := (s.BitLen-1)/bitStrWordSize + 1
		wordFmt := "%08x"
		if c == 'b' {
			wordFmt = "%032b"
		}
		for _, w := range s.Words[:n] {
			fmt.Fprintf(f, wordFmt, w)
		}
	}
}

type brtNode struct {
	zPfx, oPfx     bitStr
	zChild, oChild *brtNode
	data           interface{}
}

func (n *brtNode) FindPrefix(str bitStr) interface{} {
	var lastHasData *brtNode
	for str.BitLen > 0 && n != nil {
		if n.data != nil {
			lastHasData = n
		}
		if str.Bit(0) { // 1
			l := n.oPfx.BitLen
			if str.CommPfxLen(n.oPfx) != l {
				break
			}
			n = n.oChild
			str = str.Substr(l, str.BitLen-l)
		} else { // 0
			l := n.zPfx.BitLen
			if str.CommPfxLen(n.zPfx) != l {
				break
			}
			n = n.zChild
			str = str.Substr(l, str.BitLen-l)
		}
	}

	if n != nil && n.data != nil {
		return n.data
	} else if lastHasData != nil {
		return lastHasData.data
	} else {
		return nil
	}
}

func (n *brtNode) Insert(str bitStr, data interface{}) {
	newNode := &brtNode{data: data}
	var prev *brtNode
	key := str
	for {
		if str.BitLen == 0 {
			panic("duplicated key found: " + fmt.Sprint(key))
		}
		if n == nil {
			if str.Bit(0) { // 1
				prev.oChild = newNode
				prev.oPfx = str
			} else {
				prev.zChild = newNode
				prev.zPfx = str
			}
			return
		}

		nPfxPtr := &n.zPfx
		nChildPtr := &n.zChild
		if str.Bit(0) {
			nPfxPtr = &n.oPfx
			nChildPtr = &n.oChild
		}

		l := nPfxPtr.BitLen
		cpl := nPfxPtr.CommPfxLen(str)
		if cpl == l { // pfx matched
			prev, n = n, *nChildPtr
			str = str.Substr(l, str.BitLen-l)
		} else { // pfx not matched
			newNodePfx := str.Substr(cpl, str.BitLen-cpl)
			newParent := &brtNode{}
			if nPfxPtr.Bit(cpl) { // 1
				if newNodePfx.BitLen > 0 {
					newParent.zChild = newNode
					newParent.zPfx = newNodePfx
				}
				newParent.oChild = *nChildPtr
				newParent.oPfx = nPfxPtr.Substr(cpl, l-cpl)
			} else { // 0
				if newNodePfx.BitLen > 0 {
					newParent.oChild = newNode
					newParent.oPfx = newNodePfx
				}
				newParent.zChild = *nChildPtr
				newParent.zPfx = nPfxPtr.Substr(cpl, l-cpl)
			}
			if newNodePfx.BitLen == 0 {
				newParent.data = data
			}
			*nPfxPtr = nPfxPtr.Substr(0, cpl)
			*nChildPtr = newParent
			return
		}
	}
}

func (n *brtNode) String() string {
	buf := bytes.NewBufferString("")
	var printNode func(n *brtNode, pfx string)
	printNode = func(n *brtNode, pfx string) {
		buf.WriteString(pfx)
		fmt.Fprintf(buf, "val: %v\n", n.data)
		fmt.Fprintf(buf, "%s|-0: %v\n", pfx, n.zPfx)
		if n.zChild != nil {
			printNode(n.zChild, pfx+"| ")
			fmt.Fprintln(buf, pfx+"| ")
		}
		fmt.Fprintf(buf, "%s|-1: %v\n", pfx, n.oPfx)
		if n.oChild != nil {
			printNode(n.oChild, pfx+"| ")
		}
	}
	printNode(n, "")
	return buf.String()
}
