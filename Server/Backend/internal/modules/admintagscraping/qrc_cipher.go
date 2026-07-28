package admintagscraping

// QQ cloud QRC uses the DES-compatible cipher implemented by LDDC. Its S-box
// tables differ from the standard library's DES implementation.

const (
	qrcCipherEncrypt = 1
	qrcCipherDecrypt = 0
)

type qrcRoundKey [16][6]byte
type qrcTripleRoundKey [3]qrcRoundKey

var qrcSBoxes = [8][64]uint32{
	{
		14, 4, 13, 1, 2, 15, 11, 8, 3, 10, 6, 12, 5, 9, 0, 7,
		0, 15, 7, 4, 14, 2, 13, 1, 10, 6, 12, 11, 9, 5, 3, 8,
		4, 1, 14, 8, 13, 6, 2, 11, 15, 12, 9, 7, 3, 10, 5, 0,
		15, 12, 8, 2, 4, 9, 1, 7, 5, 11, 3, 14, 10, 0, 6, 13,
	},
	{
		15, 1, 8, 14, 6, 11, 3, 4, 9, 7, 2, 13, 12, 0, 5, 10,
		3, 13, 4, 7, 15, 2, 8, 15, 12, 0, 1, 10, 6, 9, 11, 5,
		0, 14, 7, 11, 10, 4, 13, 1, 5, 8, 12, 6, 9, 3, 2, 15,
		13, 8, 10, 1, 3, 15, 4, 2, 11, 6, 7, 12, 0, 5, 14, 9,
	},
	{
		10, 0, 9, 14, 6, 3, 15, 5, 1, 13, 12, 7, 11, 4, 2, 8,
		13, 7, 0, 9, 3, 4, 6, 10, 2, 8, 5, 14, 12, 11, 15, 1,
		13, 6, 4, 9, 8, 15, 3, 0, 11, 1, 2, 12, 5, 10, 14, 7,
		1, 10, 13, 0, 6, 9, 8, 7, 4, 15, 14, 3, 11, 5, 2, 12,
	},
	{
		7, 13, 14, 3, 0, 6, 9, 10, 1, 2, 8, 5, 11, 12, 4, 15,
		13, 8, 11, 5, 6, 15, 0, 3, 4, 7, 2, 12, 1, 10, 14, 9,
		10, 6, 9, 0, 12, 11, 7, 13, 15, 1, 3, 14, 5, 2, 8, 4,
		3, 15, 0, 6, 10, 10, 13, 8, 9, 4, 5, 11, 12, 7, 2, 14,
	},
	{
		2, 12, 4, 1, 7, 10, 11, 6, 8, 5, 3, 15, 13, 0, 14, 9,
		14, 11, 2, 12, 4, 7, 13, 1, 5, 0, 15, 10, 3, 9, 8, 6,
		4, 2, 1, 11, 10, 13, 7, 8, 15, 9, 12, 5, 6, 3, 0, 14,
		11, 8, 12, 7, 1, 14, 2, 13, 6, 15, 0, 9, 10, 4, 5, 3,
	},
	{
		12, 1, 10, 15, 9, 2, 6, 8, 0, 13, 3, 4, 14, 7, 5, 11,
		10, 15, 4, 2, 7, 12, 9, 5, 6, 1, 13, 14, 0, 11, 3, 8,
		9, 14, 15, 5, 2, 8, 12, 3, 7, 0, 4, 10, 1, 13, 11, 6,
		4, 3, 2, 12, 9, 5, 15, 10, 11, 14, 1, 7, 6, 0, 8, 13,
	},
	{
		4, 11, 2, 14, 15, 0, 8, 13, 3, 12, 9, 7, 5, 10, 6, 1,
		13, 0, 11, 7, 4, 9, 1, 10, 14, 3, 5, 12, 2, 15, 8, 6,
		1, 4, 11, 13, 12, 3, 7, 14, 10, 15, 6, 8, 0, 5, 9, 2,
		6, 11, 13, 8, 1, 4, 10, 7, 9, 5, 0, 15, 14, 2, 3, 12,
	},
	{
		13, 2, 8, 4, 6, 15, 11, 1, 10, 9, 3, 14, 5, 0, 12, 7,
		1, 15, 13, 8, 10, 3, 7, 4, 12, 5, 6, 11, 0, 14, 9, 2,
		7, 11, 4, 1, 9, 12, 14, 2, 0, 6, 10, 13, 15, 3, 5, 8,
		2, 1, 14, 7, 4, 10, 8, 13, 15, 12, 9, 0, 3, 5, 6, 11,
	},
}

var (
	qrcInitialLeft = [...]int{
		57, 49, 41, 33, 25, 17, 9, 1, 59, 51, 43, 35, 27, 19, 11, 3,
		61, 53, 45, 37, 29, 21, 13, 5, 63, 55, 47, 39, 31, 23, 15, 7,
	}
	qrcInitialRight = [...]int{
		56, 48, 40, 32, 24, 16, 8, 0, 58, 50, 42, 34, 26, 18, 10, 2,
		60, 52, 44, 36, 28, 20, 12, 4, 62, 54, 46, 38, 30, 22, 14, 6,
	}
	qrcFinalPermutation = [...]int{
		15, 6, 19, 20, 28, 11, 27, 16, 0, 14, 22, 25, 4, 17, 30, 9,
		1, 7, 23, 13, 31, 26, 2, 8, 18, 12, 29, 5, 21, 10, 3, 24,
	}
	qrcKeyRoundShift   = [...]uint{1, 1, 2, 2, 2, 2, 2, 2, 1, 2, 2, 2, 2, 2, 2, 1}
	qrcKeyPermutationC = [...]int{
		56, 48, 40, 32, 24, 16, 8, 0, 57, 49, 41, 33, 25, 17, 9, 1,
		58, 50, 42, 34, 26, 18, 10, 2, 59, 51, 43, 35,
	}
	qrcKeyPermutationD = [...]int{
		62, 54, 46, 38, 30, 22, 14, 6, 61, 53, 45, 37, 29, 21, 13, 5,
		60, 52, 44, 36, 28, 20, 12, 4, 27, 19, 11, 3,
	}
	qrcKeyCompression = [...]int{
		13, 16, 10, 23, 0, 4, 2, 27, 14, 5, 20, 9, 22, 18, 11, 3,
		25, 7, 15, 6, 26, 19, 12, 1, 40, 51, 30, 36, 46, 54, 29, 39,
		50, 44, 32, 47, 43, 48, 38, 55, 33, 52, 45, 41, 49, 35, 28, 31,
	}
)

func qrcBitnum(input []byte, bit, shift int) uint32 {
	return uint32((input[(bit/32)*4+3-(bit%32)/8]>>uint(7-bit%8))&1) << uint(shift)
}

func qrcBitnumIntr(value uint32, bit, shift int) uint32 {
	return ((value >> uint(31-bit)) & 1) << uint(shift)
}

func qrcBitnumIntl(value uint32, bit, shift int) uint32 {
	return ((value << uint(bit)) & 0x80000000) >> uint(shift)
}

func qrcSBoxBit(value uint32) int {
	return int((value & 32) | ((value & 31) >> 1) | ((value & 1) << 4))
}

func qrcInitialPermutation(input []byte) (uint32, uint32) {
	var left, right uint32
	for index, bit := range qrcInitialLeft {
		left |= qrcBitnum(input, bit, 31-index)
	}
	for index, bit := range qrcInitialRight {
		right |= qrcBitnum(input, bit, 31-index)
	}
	return left, right
}

func qrcInversePermutation(left, right uint32) []byte {
	result := make([]byte, 8)
	for group := 0; group < 8; group++ {
		outputByte := (group + 4) % 8
		var value uint32
		for index := 0; index < 4; index++ {
			bit := group + 8*index
			value |= qrcBitnumIntr(right, bit, 7-2*index)
			value |= qrcBitnumIntr(left, bit, 6-2*index)
		}
		result[outputByte] = byte(value)
	}
	return result
}

func qrcRoundFunction(state uint32, key [6]byte) uint32 {
	t1 := qrcBitnumIntl(state, 31, 0) | (state&0xf0000000)>>1 |
		qrcBitnumIntl(state, 4, 5) | qrcBitnumIntl(state, 3, 6) | (state&0x0f000000)>>3 |
		qrcBitnumIntl(state, 8, 11) | qrcBitnumIntl(state, 7, 12) | (state&0x00f00000)>>5 |
		qrcBitnumIntl(state, 12, 17) | qrcBitnumIntl(state, 11, 18) | (state&0x000f0000)>>7 |
		qrcBitnumIntl(state, 16, 23)
	t2 := qrcBitnumIntl(state, 15, 0) | (state&0x0000f000)<<15 |
		qrcBitnumIntl(state, 20, 5) | qrcBitnumIntl(state, 19, 6) | (state&0x00000f00)<<13 |
		qrcBitnumIntl(state, 24, 11) | qrcBitnumIntl(state, 23, 12) | (state&0x000000f0)<<11 |
		qrcBitnumIntl(state, 28, 17) | qrcBitnumIntl(state, 27, 18) | (state&0x0000000f)<<9 |
		qrcBitnumIntl(state, 0, 23)

	var expanded [6]uint32
	expanded[0] = (t1 >> 24) & 0xff
	expanded[1] = (t1 >> 16) & 0xff
	expanded[2] = (t1 >> 8) & 0xff
	expanded[3] = (t2 >> 24) & 0xff
	expanded[4] = (t2 >> 16) & 0xff
	expanded[5] = (t2 >> 8) & 0xff
	for index := range expanded {
		expanded[index] ^= uint32(key[index])
	}

	permuted :=
		(qrcSBoxes[0][qrcSBoxBit(expanded[0]>>2)] << 28) |
			(qrcSBoxes[1][qrcSBoxBit(((expanded[0]&3)<<4)|(expanded[1]>>4))] << 24) |
			(qrcSBoxes[2][qrcSBoxBit(((expanded[1]&0xf)<<2)|(expanded[2]>>6))] << 20) |
			(qrcSBoxes[3][qrcSBoxBit(expanded[2]&0x3f)] << 16) |
			(qrcSBoxes[4][qrcSBoxBit(expanded[3]>>2)] << 12) |
			(qrcSBoxes[5][qrcSBoxBit(((expanded[3]&3)<<4)|(expanded[4]>>4))] << 8) |
			(qrcSBoxes[6][qrcSBoxBit(((expanded[4]&0xf)<<2)|(expanded[5]>>6))] << 4) |
			qrcSBoxes[7][qrcSBoxBit(expanded[5]&0x3f)]

	var result uint32
	for shift, bit := range qrcFinalPermutation {
		result |= qrcBitnumIntl(permuted, bit, shift)
	}
	return result
}

func qrcCryptBlock(input []byte, key qrcRoundKey) []byte {
	left, right := qrcInitialPermutation(input)
	for index := 0; index < 15; index++ {
		previousRight := right
		right = qrcRoundFunction(right, key[index]) ^ left
		left = previousRight
	}
	left = qrcRoundFunction(right, key[15]) ^ left
	return qrcInversePermutation(left, right)
}

func qrcKeySchedule(key []byte, mode int) qrcRoundKey {
	var schedule qrcRoundKey
	var left, right uint32
	for index := 0; index < 28; index++ {
		left |= qrcBitnum(key, qrcKeyPermutationC[index], 31-index)
		right |= qrcBitnum(key, qrcKeyPermutationD[index], 31-index)
	}
	for index, shift := range qrcKeyRoundShift {
		left = ((left << shift) | (left >> (28 - shift))) & 0xfffffff0
		right = ((right << shift) | (right >> (28 - shift))) & 0xfffffff0
		generated := index
		if mode == qrcCipherDecrypt {
			generated = 15 - index
		}
		for compressionIndex := 0; compressionIndex < 24; compressionIndex++ {
			schedule[generated][compressionIndex/8] |= byte(qrcBitnumIntr(left, qrcKeyCompression[compressionIndex], 7-compressionIndex%8))
		}
		for compressionIndex := 24; compressionIndex < 48; compressionIndex++ {
			bit := qrcKeyCompression[compressionIndex] - 27
			schedule[generated][compressionIndex/8] |= byte(qrcBitnumIntr(right, bit, 7-compressionIndex%8))
		}
	}
	return schedule
}

func qrcTripleKeySchedule(key []byte, mode int) qrcTripleRoundKey {
	var schedule qrcTripleRoundKey
	if mode == qrcCipherEncrypt {
		schedule[0] = qrcKeySchedule(key[0:8], qrcCipherEncrypt)
		schedule[1] = qrcKeySchedule(key[8:16], qrcCipherDecrypt)
		schedule[2] = qrcKeySchedule(key[16:24], qrcCipherEncrypt)
		return schedule
	}
	schedule[0] = qrcKeySchedule(key[16:24], qrcCipherDecrypt)
	schedule[1] = qrcKeySchedule(key[8:16], qrcCipherEncrypt)
	schedule[2] = qrcKeySchedule(key[0:8], qrcCipherDecrypt)
	return schedule
}

func qrcTripleCryptBlock(input []byte, key qrcTripleRoundKey) []byte {
	result := append([]byte(nil), input[:8]...)
	for index := range key {
		result = qrcCryptBlock(result, key[index])
	}
	return result
}
