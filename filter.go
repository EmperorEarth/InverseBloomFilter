/*
Package filter implements a concurrent "inverse" Bloom filter, which is
effectively the opposite of a classical Bloom filter. It may report a false
negative but can never report a false positive. That is, it may report that an
item has not been seen when it actually has, but it will never report an item
as seen which it hasn't come across. This behaves in a similar manner to a
fixed-size hashmap which does not handle conflicts. An example use case is
de-duplicating events while processing a stream of data. Ideally, duplicate
events are relatively close together.

Credits go to Jeff Hodges
(http://www.somethingsimilar.com/2012/05/21/the-opposite-of-a-bloom-filter/).
*/
package filter

import (
	"bytes"
	"errors"
	"hash"
	"hash/fnv"
	"math"
	"sync/atomic"
	"unsafe"
)

var (
	// ErrSizeTooLarge is returned by NewSet when the specified size is too
	// large to allocate.
	ErrSizeTooLarge = errors.New("Size given too large to round to a power of 2")

	// ErrSizeTooSmall is returned by NewSet when the specified size is less
	// than or equal to zero.
	ErrSizeTooSmall = errors.New("Cannot have a zero or negative size")

	// MaxSize indicates the largest possible filter size.
	MaxSize = 1 << 30
)

// InverseBloomFilter is a concurrent, probabilistic data structure which can
// be thought of as the "opposite" of a Bloom filter. It may report a false
// negative but can never report a false positive.
type InverseBloomFilter struct {
	array    []*[]byte
	sizeMask uint32
	hash     *uintHash
}

// NewInverseBloomFilter creates and returns a new InverseBloomFilter with the
// specified capacity. It returns an error if the size is not between 0 and
// MaxSize.
func NewInverseBloomFilter(size int) (*InverseBloomFilter, error) {
	if size > MaxSize {
		return nil, ErrSizeTooLarge
	}
	if size <= 0 {
		return nil, ErrSizeTooSmall
	}

	// Round to the next largest power of two.
	size = int(math.Pow(2, math.Ceil(math.Log2(float64(size)))))
	slice := make([]*[]byte, size)
	sizeMask := uint32(size - 1)
	return &InverseBloomFilter{slice, sizeMask, &uintHash{fnv.New32a()}}, nil
}

// Observe marks a key as observed. It returns true if the key has been
// previously observed and false if the key has possibly not been observed
// yet. It may report a false negative but will never report a false positive.
// That is, it may return false even though the key was previously observed,
// but it will never return true for a key that has never been observed.
func (i *InverseBloomFilter) Observe(key []byte) bool {
	i.hash.Write(key)
	uindex := i.hash.Sum32() & i.sizeMask
	i.hash.Reset()
	oldId := getAndSet(i.array, int32(uindex), key)
	return bytes.Equal(oldId, key)
}

// Size returns the filter length.
func (i *InverseBloomFilter) Size() int {
	return len(i.array)
}

type uintHash struct {
	hash.Hash
}

func (u uintHash) Sum32() uint32 {
	sum := u.Sum(nil)
	x := uint32(sum[0])
	for _, val := range sum[1:3] {
		x = x << 3
		x += uint32(val)
	}
	return x
}

// getAndSet returns the key that was in the slice at the given index after
// putting the new key in the slice at that index, atomically.
func getAndSet(arr []*[]byte, index int32, key []byte) []byte {
	indexPtr := (*unsafe.Pointer)(unsafe.Pointer(&arr[index]))
	keyUnsafe := unsafe.Pointer(&key)
	var oldKey []byte
	for {
		oldKeyUnsafe := atomic.LoadPointer(indexPtr)
		if atomic.CompareAndSwapPointer(indexPtr, oldKeyUnsafe, keyUnsafe) {
			oldKeyPtr := (*[]byte)(oldKeyUnsafe)
			if oldKeyPtr != nil {
				oldKey = *oldKeyPtr
			}
			break
		}
	}
	return oldKey
}
