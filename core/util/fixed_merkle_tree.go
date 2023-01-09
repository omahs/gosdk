package util

import (
	"bytes"
	"encoding/hex"
	"hash"
	"io"
	"sync"

	goError "errors"

	"github.com/0chain/errors"
	"golang.org/x/crypto/sha3"
)

const (
	MerkleChunkSize     = 64
	MaxMerkleLeavesSize = 64 * 1024
	FixedMerkleLeaves   = 1024
	FixedMTDepth        = 11
)

type leaf struct {
	h hash.Hash
}

func (l *leaf) GetHashBytes() []byte {
	return l.h.Sum(nil)
}

func (l *leaf) GetHash() string {
	return hex.EncodeToString(l.h.Sum(nil))
}

func (l *leaf) Write(b []byte) (int, error) {
	return l.h.Write(b)
}

func getNewLeaf() *leaf {
	return &leaf{
		h: sha3.New256(),
	}
}

// FixedMerkleTree A trusted mekerl tree for outsourcing attack protection. see section 1.8 on whitepager
// see detail on https://github.com/0chain/blobber/wiki/Protocols#what-is-fixedmerkletree
type FixedMerkleTree struct {
	// ChunkSize size of chunk
	Leaves []Hashable `json:"leaves,omitempty"`

	writeLock  *sync.Mutex
	isFinal    bool
	writeCount int
	writeBytes []byte
	merkleRoot []byte
}

func (fmt *FixedMerkleTree) Finalize() error {
	fmt.writeLock.Lock()
	if fmt.isFinal {
		return goError.New("already finalized")
	}
	fmt.isFinal = true
	fmt.writeLock.Unlock()
	if fmt.writeCount > 0 {
		return fmt.writeToLeaves(fmt.writeBytes[:fmt.writeCount])
	}
	return nil
}

// NewFixedMerkleTree create a FixedMerkleTree with specify hash method
func NewFixedMerkleTree() *FixedMerkleTree {

	t := &FixedMerkleTree{
		writeBytes: make([]byte, MaxMerkleLeavesSize),
		writeLock:  &sync.Mutex{},
	}
	t.initLeaves()

	return t

}

func (fmt *FixedMerkleTree) initLeaves() {
	fmt.Leaves = make([]Hashable, FixedMerkleLeaves)
	for i := 0; i < FixedMerkleLeaves; i++ {
		fmt.Leaves[i] = getNewLeaf()
	}
}

func (fmt *FixedMerkleTree) writeToLeaves(b []byte) error {
	if len(b) > MaxMerkleLeavesSize {
		return goError.New("data size greater than maximum required size")
	}

	if len(b) < MaxMerkleLeavesSize && !fmt.isFinal {
		return goError.New("invalid merkle leaf write")
	}

	leafInd := 0
	for i := 0; i < len(b); i += MerkleChunkSize {
		j := i + MerkleChunkSize
		if j > len(b) {
			j = len(b)
		}

		fmt.Leaves[leafInd].Write(b[i:j])
		leafInd++
	}

	return nil
}

func (fmt *FixedMerkleTree) Write(b []byte) (int, error) {

	fmt.writeLock.Lock()
	defer fmt.writeLock.Unlock()
	if fmt.isFinal {
		return 0, goError.New("cannot write. Tree is already finalized")
	}

	for i, j := 0, MaxMerkleLeavesSize-fmt.writeCount; i < len(b); i, j = j, j+MaxMerkleLeavesSize {
		if j > len(b) {
			j = len(b)
		}
		prevWriteCount := fmt.writeCount
		fmt.writeCount += int(j - i)
		copy(fmt.writeBytes[prevWriteCount:fmt.writeCount], b[i:j])
		if fmt.writeCount == MaxMerkleLeavesSize {
			err := fmt.writeToLeaves(fmt.writeBytes)
			if err != nil {
				return 0, err
			}
			fmt.writeCount = 0
		}
	}
	return len(b), nil
}

// GetMerkleRoot get merkle tree
func (fmt *FixedMerkleTree) GetMerkleTree() MerkleTreeI {
	return nil
}

func (fmt *FixedMerkleTree) CalculateMerkleRoot() {
	nodes := make([][]byte, len(fmt.Leaves))
	for i := 0; i < len(nodes); i++ {
		nodes[i] = fmt.Leaves[i].GetHashBytes()
	}

	for i := 0; i < FixedMTDepth; i++ {

		newNodes := make([][]byte, (len(nodes)+1)/2)
		if len(nodes)&1 == 1 {
			nodes = append(nodes, nodes[len(nodes)-1])
		}

		nodeInd := 0
		for j := 0; j < len(nodes); j += 2 {
			newNodes[nodeInd] = MHashBytes(nodes[j], nodes[j+1])
			nodeInd++
		}
		nodes = newNodes
		if len(nodes) == 1 {
			break
		}
	}

	fmt.merkleRoot = nodes[0]
}

type FixedMerklePath struct {
	LeafHash []byte   `json:"leaf_hash"`
	RootHash []byte   `json:"root_hash"`
	Nodes    [][]byte `json:"nodes"`
	LeafInd  int
}

func (fp FixedMerklePath) VerifyMerklePath() bool {
	leafInd := fp.LeafInd
	hash := fp.LeafHash
	for i := 0; i < len(fp.Nodes); i++ {
		if leafInd&1 == 0 {
			hash = MHashBytes(hash, fp.Nodes[i])
		} else {
			hash = MHashBytes(fp.Nodes[i], hash)
		}
		leafInd = leafInd / 2
	}
	return bytes.Equal(hash, fp.RootHash)
}

// GetMerkleRoot get merkle root
func (fmt *FixedMerkleTree) GetMerkleRoot() string {
	if fmt.merkleRoot != nil {
		return hex.EncodeToString(fmt.merkleRoot)
	}
	fmt.CalculateMerkleRoot()
	return hex.EncodeToString(fmt.merkleRoot)
}

// Reload reset and reload leaves from io.Reader
func (fmt *FixedMerkleTree) Reload(reader io.Reader) error {

	fmt.initLeaves()

	bytesBuf := bytes.NewBuffer(make([]byte, 0, MaxMerkleLeavesSize))
	for i := 0; ; i++ {
		written, err := io.CopyN(bytesBuf, reader, MaxMerkleLeavesSize)

		if written > 0 {
			_, err = fmt.Write(bytesBuf.Bytes())
			bytesBuf.Reset()

			if err != nil {
				return err
			}

		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return err
		}

	}

	return nil
}
