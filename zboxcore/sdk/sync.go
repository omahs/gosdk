package sdk

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"

	"os"
	"path/filepath"
	"strings"

	"github.com/0chain/gosdk/zboxcore/fileref"
	. "github.com/0chain/gosdk/zboxcore/logger"
)

// For sync app
const (
	Upload      = "Upload"
	Download    = "Download"
	Update      = "Update"
	Delete      = "Delete"
	Conflict    = "Conflict"
	LocalDelete = "LocalDelete"
)

type fileInfo struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Hash string `json:"hash"`
}

type FileDiff struct {
	Op   string `json:"operation"`
	Path string `json:"path"`
}

func (a *Allocation) getRemoteFilesAndDirs(dirList []string, fileList *[]fileInfo, exclMap map[string]int) ([]string, error) {
	childDirList := make([]string, 0)
	for _, dir := range dirList {
		ref, err := a.ListDir(dir)
		if err != nil {
			return []string{}, err
		}
		for _, child := range ref.Children {
			if _, ok := exclMap[child.Path]; ok {
				continue
			}
			if child.Type == fileref.FILE {
				*fileList = append(*fileList, fileInfo{Path: child.Path, Size: child.Size, Hash: child.Hash})
			} else {
				childDirList = append(childDirList, child.Path)
			}
		}
	}
	return childDirList, nil
}

func (a *Allocation) getRemoteFileList(exclMap map[string]int) ([]fileInfo, error) {
	// 1. Iteratively get dir and files seperately till no more dirs left
	var remoteList []fileInfo
	dirs := []string{"/"}
	var err error
	for {
		dirs, err = a.getRemoteFilesAndDirs(dirs, &remoteList, exclMap)
		if err != nil {
			fmt.Println(err.Error())
			break
		}
		if len(dirs) == 0 {
			break
		}
	}
	return remoteList, err
}

func calcFileHash(filePath string) string {
	fp, err := os.Open(filePath)
	if err != nil {
		log.Fatal(err)
	}
	defer fp.Close()

	h := sha1.New()
	if _, err := io.Copy(h, fp); err != nil {
		log.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func getRemoteExcludeMap(exclPath []string) map[string]int {
	exclMap := make(map[string]int)
	for idx, path := range exclPath {
		exclMap[strings.TrimRight(path, "/")] = idx
	}
	return exclMap
}

func addLocalFileList(root string, fileList *[]fileInfo, filter map[string]bool, exclMap map[string]int) filepath.WalkFunc {
	return func(path string, info os.FileInfo, err error) error {
		if err != nil {
			Logger.Error("Local file list error for path", path, err.Error())
			return nil
		}
		// Filter out
		if _, ok := filter[info.Name()]; ok {
			return nil
		}
		rPath := "/" + strings.TrimLeft(path, root)
		// Exclude
		if _, ok := exclMap[rPath]; ok {
			return nil
		}
		// Add to list
		if !info.IsDir() {
			*fileList = append(*fileList, fileInfo{Path: rPath, Size: info.Size(), Hash: calcFileHash(path)})
		}
		return nil
	}
}

func getLocalFileList(rootPath string, filters []string, exclMap map[string]int) ([]fileInfo, error) {
	var localList []fileInfo
	filterMap := make(map[string]bool)
	for _, f := range filters {
		filterMap[f] = true
	}
	err := filepath.Walk(rootPath, addLocalFileList(rootPath, &localList, filterMap, exclMap))
	return localList, err
}

func findDelta(remote []fileInfo, local []fileInfo, prevRemote []fileInfo) []FileDiff {
	var lFDiff []FileDiff
	// Create previous remote list as map
	prevMap := make(map[string]string)
	for _, f := range prevRemote {
		prevMap[f.Path] = f.Hash
	}

	// Create a remote hash map and find modifications
	rMap := make(map[string]string)
	rMod := make(map[string]string)
	for _, rFile := range remote {
		rMap[rFile.Path] = rFile.Hash
		if hash, ok := prevMap[rFile.Path]; ok {
			// Remote file existed in previous sync also
			if hash != rFile.Hash {
				// File modified in remote
				rMod[rFile.Path] = rFile.Hash
			}
		}
	}

	// Create a local hash map and find modification
	lMap := make(map[string]string)
	lMod := make(map[string]string)
	for _, lFile := range local {
		lMap[lFile.Path] = lFile.Hash
		if hash, ok := prevMap[lFile.Path]; ok {
			// Local file existed in previous sync also
			if hash != lFile.Hash {
				// File modified in local
				lMod[lFile.Path] = lFile.Hash
			}
		}
	}

	// Iterate remote list and get diff
	for rPath, _ := range rMap {
		op := Download
		bRemoteModified := false
		bLocalModified := false
		if _, ok := rMod[rPath]; ok {
			bRemoteModified = true
		}
		if _, ok := lMod[rPath]; ok {
			bLocalModified = true
			delete(lMap, rPath)
		}
		if bRemoteModified && bLocalModified {
			op = Conflict
		} else if bLocalModified {
			op = Update
		} else if _, ok := lMap[rPath]; ok {
			// No conflicts and file exists locally
			delete(lMap, rPath)
			continue
		} else if _, ok := prevMap[rPath]; ok {
			op = Delete
		}
		lFDiff = append(lFDiff, FileDiff{Path: rPath, Op: op})
	}

	// Upload all local files
	for lPath, _ := range lMap {
		op := Upload
		if _, ok := lMod[lPath]; ok {
			op = Update
		} else if _, ok := prevMap[lPath]; ok {
			op = LocalDelete
		}
		lFDiff = append(lFDiff, FileDiff{Path: lPath, Op: op})
	}

	return lFDiff
}

func (a *Allocation) GetAllocationDiff(lastSyncCachePath string, localRootPath string, localFileFilters []string, remoteExcludePath []string) ([]FileDiff, error) {
	var lFdiff []FileDiff
	var prevRemoteFileList []fileInfo
	// 1. Validate localSycnCachePath
	if len(lastSyncCachePath) > 0 {
		// Validate cache path
		fileInfo, err := os.Stat(lastSyncCachePath)
		if err == nil {
			if fileInfo.IsDir() {
				return lFdiff, fmt.Errorf("invalid file cache. %v", err)
			}
			content, err := ioutil.ReadFile(lastSyncCachePath)
			if err != nil {
				return lFdiff, fmt.Errorf("can't read cache file.")
			}
			err = json.Unmarshal(content, &prevRemoteFileList)
			if err != nil {
				return lFdiff, fmt.Errorf("invalid cache content.")
			}
		}
	}

	// 2. Build a map for exclude path
	exclMap := getRemoteExcludeMap(remoteExcludePath)

	// 3. Get flat file list from remote
	remoteFileList, err := a.getRemoteFileList(exclMap)
	if err != nil {
		return lFdiff, fmt.Errorf("error getting list dir from remote. %v", err)
	}

	// 4. Get flat file list on the local filesystem
	localRootPath = strings.TrimRight(localRootPath, "/")
	localFileList, err := getLocalFileList(localRootPath, localFileFilters, exclMap)
	if err != nil {
		return lFdiff, fmt.Errorf("error getting list dir from local. %v", err)
	}

	// 5. Get the file diff with operation
	lFdiff = findDelta(remoteFileList, localFileList, prevRemoteFileList)

	return lFdiff, nil
}

// SaveRemoteSnapShot - Saves the remote current information to the given file
// This file can be passed to GetAllocationDiff to exactly find the previous sync state to current.
func (a *Allocation) SaveRemoteSnapshot(pathToSave string, remoteExcludePath []string) error {
	bIsFileExists := false
	// Validate path
	fileInfo, err := os.Stat(pathToSave)
	if err == nil {
		if fileInfo.IsDir() {
			return fmt.Errorf("invalid file path to save. %v", err)
		}
		bIsFileExists = true
	}

	// Get flat file list from remote
	exclMap := getRemoteExcludeMap(remoteExcludePath)
	remoteFileList, err := a.getRemoteFileList(exclMap)
	if err != nil {
		return fmt.Errorf("error getting list dir from remote. %v", err)
	}

	// Now we got the list from remote, delete the file if exists
	if bIsFileExists {
		err = os.Remove(pathToSave)
		if err != nil {
			return fmt.Errorf("error deleting previous cache. %v", err)
		}
	}
	by, err := json.Marshal(remoteFileList)
	if err != nil {
		return fmt.Errorf("failed to convert JSON. %v", err)
	}
	err = ioutil.WriteFile(pathToSave, by, 0644)
	if err != nil {
		return fmt.Errorf("error saving file. %v", err)
	}
	// Successfully saved
	return nil
}
