package server

import (
	"archive/zip"
	log "github.com/sjqzhang/seelog"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type MergeRequest struct {
	Folder   string    `json:"folder"`
	FileList []FileMap `json:"file_list"`
}

type FileMap struct {
	FolderName string   `json:"folder_name"`
	Files      []string `json:"files"`
}

// MergeDownload 合并文件到压缩文件
// Method: POST
// Param: folder string
// Param: file_list { folderName string, files []string }
func (c *Server) MergeDownload(w http.ResponseWriter, r *http.Request) {
	tp := STORE_DIR + "/_temp/" + time.Now().Format("20060102")
	if !c.util.FileExists(tp) {
		if err := os.MkdirAll(tp, 0777); err != nil {
			_ = log.Error(err)
		}
	}

	mr := new(MergeRequest)
	if err := json.NewDecoder(r.Body).Decode(&mr); err != nil {
		_ = log.Error(err)
		return
	}

	zp := filepath.Join(tp, mr.Folder+".zip")
	zf, err := os.Create(zp)
	if err != nil {
		_ = log.Error(err)
	}
	zfw := zip.NewWriter(zf)

	for i := 0; i < len(mr.FileList); i++ {
		merge := mr.FileList[i]
		files := merge.Files
		for j := 0; j < len(files); j++ {
			file := files[j]
			file = file[len(Config().Group)+2:]
			ftp := DOCKER_DIR + STORE_DIR_NAME + "/" + file
			if ftp, err = url.PathUnescape(ftp); err != nil {
				_ = log.Error(err)
			}
			ztp := filepath.Join(mr.Folder, merge.FolderName, filepath.Base(ftp))
			zt, createErr := zfw.Create(ztp)
			if createErr != nil {
				_ = log.Error(createErr)
			}
			ft, openErr := os.Open(ftp)
			if openErr != nil {
				_ = log.Error(openErr)
			}
			_, copyErr := io.Copy(zt, ft)
			if copyErr != nil {
				_ = log.Error(copyErr)
			}
		}
	}
	// 写入到磁盘
	_ = zfw.Close()
	_ = zf.Close()

	// 重新打开提供下载
	zf, _ = os.Open(zp)
	zfs, _ := zf.Stat()
	defer func() {
		_ = zf.Close()
	}()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+url.QueryEscape(mr.Folder+".zip")+"\"")
	w.Header().Set("Content-Length", strconv.FormatInt(zfs.Size(), 10))
	buffer := make([]byte, 1024)
	if _, err = io.CopyBuffer(w, zf, buffer); err != nil {
		_ = log.Error()
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	return
}
