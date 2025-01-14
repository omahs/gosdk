package sdk

import (
	"testing"
)

func TestMaxBlobbersRequiredGreaterThanImplicitLimit128(t *testing.T) {
	var maxNumOfBlobbers = 129

	var req = &UploadRequest{}
	req.setUploadMask(maxNumOfBlobbers)
	req.fullconsensus = maxNumOfBlobbers

	if req.IsFullConsensusSupported() {
		t.Errorf("IsFullConsensusSupported() = %v, want %v", true, false)
	}
}

func TestNaxBlobbersRequiredEqualToImplicitLimit32(t *testing.T) {
	var maxNumOfBlobbers = 32

	var req = &UploadRequest{}
	req.setUploadMask(maxNumOfBlobbers)
	req.fullconsensus = maxNumOfBlobbers

	if !req.IsFullConsensusSupported() {
		t.Errorf("IsFullConsensusSupported() = %v, want %v", false, true)
	}
}

func TestNumBlobbersRequiredGreaterThanMask(t *testing.T) {
	var maxNumOfBlobbers = 5

	var req = &UploadRequest{}
	req.setUploadMask(maxNumOfBlobbers)
	req.fullconsensus = 6

	if req.IsFullConsensusSupported() {
		t.Errorf("IsFullConsensusSupported() = %v, want %v", true, false)
	}
}

func TestNumBlobbersRequiredLessThanMask(t *testing.T) {
	var maxNumOfBlobbers = 5

	var req = &UploadRequest{}
	req.setUploadMask(maxNumOfBlobbers)
	req.fullconsensus = 4

	if !req.IsFullConsensusSupported() {
		t.Errorf("IsFullConsensusSupported() = %v, want %v", false, true)
	}
}

func TestNumBlobbersRequiredZero(t *testing.T) {
	var maxNumOfBlobbers = 5

	var req = &UploadRequest{}
	req.setUploadMask(maxNumOfBlobbers)
	req.fullconsensus = 0

	if !req.IsFullConsensusSupported() {
		t.Errorf("IsFullConsensusSupported() = %v, want %v", false, true)
	}
}
