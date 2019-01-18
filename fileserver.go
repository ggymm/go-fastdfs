package main

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/deckarep/golang-set"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net"
	"net/http"
	_ "net/http/pprof"
	"net/smtp"
	"net/url"
	"os/signal"
	"path"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strconv"
	"syscall"

	//	"strconv"
	"sync"

	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/astaxie/beego/httplib"
	log "github.com/sjqzhang/seelog"
	"github.com/syndtr/goleveldb/leveldb"
)

var staticHandler http.Handler

var server =NewServer()


var logacc log.LoggerInterface

var FOLDERS = []string{DATA_DIR, STORE_DIR, CONF_DIR}

var CONST_QUEUE_SIZE = 10000

var (
	FileName string
	ptr      unsafe.Pointer
)

const (
	STORE_DIR = "files"

	CONF_DIR = "conf"

	DATA_DIR = "data"

	CONST_LEVELDB_FILE_NAME = DATA_DIR + "/fileserver.db"

	CONST_STAT_FILE_NAME = DATA_DIR + "/stat.json"

	CONST_CONF_FILE_NAME = CONF_DIR + "/cfg.json"

	CONST_STAT_FILE_COUNT_KEY = "fileCount"

	CONST_STAT_FILE_TOTAL_SIZE_KEY = "totalSize"

	CONST_Md5_ERROR_FILE_NAME = "errors.md5"
	CONST_FILE_Md5_FILE_NAME  = "files.md5"

	cfgJson = `{
	"绑定端号": "端口",
	"addr": ":8080",
	"集群": "集群列表",
	"peers": ["%s"],
	"组号": "组号",
	"group": "group1",
	"refresh_interval": 120,
	"是否自动重命名": "真假",
	"rename_file": false,
	"是否支持ＷＥＢ上专": "真假",
	"enable_web_upload": true,
	"是否支持非日期路径": "真假",
	"enable_custom_path": true,
	"下载域名": "",
	"download_domain": "",
	"场景":"场景列表",
	"scenes":[],
	"默认场景":"",
	"default_scene":"default",
	"是否显示目录": "真假",
	"show_dir": true,
	"邮件配置":"",
	"mail":{
		"user":"abc@163.com",
		"password":"abc",
		"host":"smtp.163.com:25"
	},
	"告警接收邮件列表":"",
	"alram_receivers":[],
	"告警接收URL":"",
	"alarm_url":"",
	"下载是否需带token":"真假",
	"download_use_token":false,
	"下载token过期时间":"",
	"download_token_expire":600,
	"是否自动修复":"可能存在性问题（每小时一次）",
	"auto_repair":true
	
}
	
	`

	logConfigStr = `
<seelog type="asynctimer" asyncinterval="1000" minlevel="trace" maxlevel="error">  
	<outputs formatid="common">  
		<buffered formatid="common" size="1048576" flushperiod="1000">  
			<rollingfile type="size" filename="./log/fileserver.log" maxsize="104857600" maxrolls="10"/>  
		</buffered>
	</outputs>  	  
	 <formats>
		 <format id="common" format="%Date %Time [%LEV] [%File:%Line] [%Func] %Msg%n" />  
	 </formats>  
</seelog>
`

	logAccessConfigStr = `
<seelog type="asynctimer" asyncinterval="1000" minlevel="trace" maxlevel="error">  
	<outputs formatid="common">  
		<buffered formatid="common" size="1048576" flushperiod="1000">  
			<rollingfile type="size" filename="./log/access.log" maxsize="104857600" maxrolls="10"/>  
		</buffered>
	</outputs>  	  
	 <formats>
		 <format id="common" format="%Date %Time [%LEV] [%File:%Line] [%Func] %Msg%n" />  
	 </formats>  
</seelog>
`
)

type Common struct {
}

type Server struct {
	ldb          *leveldb.DB
	util         *Common
	statMap      *CommonMap
	queueToPeers chan FileInfo
	fileset      mapset.Set
	errorset     mapset.Set
	curDate string


}



type FileInfo struct {
	Name      string
	ReName    string
	Path      string
	Md5       string
	Size      int64
	Peers     []string
	Scene     string
	TimeStamp int64
	writeLog bool
}

type Status struct {
	Message string `json:"message"`
	Status  string `json:"status"`
}

type FileResult struct {
	Url    string `json:"url"`
	Md5    string `json:"md5"`
	Path   string `json:"path"`
	Domain string `json:"domain"`
	Scene  string `json:"scene"`
	//Just for Compatibility
	Scenes  string `json:"scenes"`
	Retmsg  string `json:"retmsg"`
	Retcode int    `json:"retcode"`
	Src     string `json:"src"`
}

type Mail struct {
	User     string `json:"user"`
	Password string `json:"password"`
	Host     string `json:"host"`
}

type StatDateFileInfo struct {
	Date      string `json:"date"`
	TotalSize int64  `json:"totalSize"`
	FileCount int64  `json:"fileCount"`
}

type GloablConfig struct {
	Addr                string   `json:"addr"`
	Peers               []string `json:"peers"`
	Group               string   `json:"group"`
	RenameFile          bool     `json:"rename_file"`
	ShowDir             bool     `json:"show_dir"`
	RefreshInterval     int      `json:"refresh_interval"`
	EnableWebUpload     bool     `json:"enable_web_upload"`
	DownloadDomain      string   `json:"download_domain"`
	EnableCustomPath    bool     `json:"enable_custom_path"`
	Scenes              []string `json:"scenes"`
	AlramReceivers      []string `json:"alram_receivers"`
	DefaultScene        string   `json:"default_scene"`
	Mail                Mail     `json:"mail"`
	AlarmUrl            string   `json:"alarm_url"`
	DownloadUseToken    bool     `json:"download_use_token"`
	DownloadTokenExpire int      `json:"download_token_expire"`
	QueueSize           int      `json:"queue_size"`
	AutoRepair            bool      `json:"auto_repair"`
}

func NewServer() *Server  {
	var (
		server *Server
		size int64
	)

	size=0

	server=&Server{
		util:         &Common{},
		statMap:      &CommonMap{m: make(map[string]interface{})},
		queueToPeers: make(chan FileInfo, CONST_QUEUE_SIZE),
		fileset:      mapset.NewSet(),
		errorset:mapset.NewSet(),
	}
	settins:=httplib.BeegoHTTPSettings{
		UserAgent:        "go-fastdfs",
		ConnectTimeout:   10 * time.Second,
		ReadWriteTimeout: 10 * time.Second,
		Gzip:             true,
		DumpBody:         true,
	}
	httplib.SetDefaultSetting(settins)
	server.statMap.Put(CONST_STAT_FILE_COUNT_KEY,size)
	server.statMap.Put(CONST_STAT_FILE_TOTAL_SIZE_KEY,size)
	server.statMap.Put(server.util.GetToDay()+"_"+CONST_STAT_FILE_COUNT_KEY,size)
	server.statMap.Put(server.util.GetToDay()+"_"+CONST_STAT_FILE_TOTAL_SIZE_KEY,size)


	server.curDate=server.util.GetToDay()

	return server


}

type CommonMap struct {
	sync.Mutex
	m map[string]interface{}
}

func (s *CommonMap) GetValue(k string) (interface{}, bool) {
	s.Lock()
	defer s.Unlock()
	v, ok := s.m[k]
	return v, ok
}

func (s *CommonMap) Put(k string, v interface{}) {
	s.Lock()
	defer s.Unlock()
	s.m[k] = v
}

func (s *CommonMap) AddCount(key string, count int) {
	s.Lock()
	defer s.Unlock()
	if _v, ok := s.m[key]; ok {
		v := _v.(int)
		v = v + count
		s.m[key] = v
	} else {
		s.m[key] = 1
	}
}

func (s *CommonMap) AddCountInt64(key string, count int64) {
	s.Lock()
	defer s.Unlock()

	if _v, ok := s.m[key]; ok {
		v := _v.(int64)
		v = v + count
		s.m[key] = v
	} else {

		s.m[key] = count
	}
}

func (s *CommonMap) Add(key string) {
	s.Lock()
	defer s.Unlock()
	if _v, ok := s.m[key]; ok {
		v := _v.(int)
		v = v + 1
		s.m[key] = v
	} else {

		s.m[key] = 1

	}
}

func (s *CommonMap) Zero() {
	s.Lock()
	defer s.Unlock()
	for k, _ := range s.m {

		s.m[k] = 0
	}
}

func (s *CommonMap) Get() map[string]interface{} {
	s.Lock()
	defer s.Unlock()
	m := make(map[string]interface{})
	for k, v := range s.m {
		m[k] = v
	}
	return m
}

func Config() *GloablConfig {
	return (*GloablConfig)(atomic.LoadPointer(&ptr))
}

func ParseConfig(filePath string) {
	var (
		data []byte
	)

	if filePath == "" {
		data = []byte(strings.TrimSpace(cfgJson))
	} else {
		file, err := os.Open(filePath)
		if err != nil {
			panic(fmt.Sprintln("open file path:", filePath, "error:", err))
		}

		defer file.Close()

		FileName = filePath

		data, err = ioutil.ReadAll(file)
		if err != nil {
			panic(fmt.Sprintln("file path:", filePath, " read all error:", err))
		}
	}

	var c GloablConfig
	if err := json.Unmarshal(data, &c); err != nil {
		panic(fmt.Sprintln("file path:", filePath, "json unmarshal error:", err))
	}

	log.Info(c)

	atomic.StorePointer(&ptr, unsafe.Pointer(&c))

	log.Info("config parse success")
}

func (this *Common) GetUUID() string {

	b := make([]byte, 48)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return ""
	}

	id := this.MD5(base64.URLEncoding.EncodeToString(b))
	return fmt.Sprintf("%s-%s-%s-%s-%s", id[0:8], id[8:12], id[12:16], id[16:20], id[20:])

}

func (this *Common) GetToDay() string {

	return time.Now().Format("20060102")

}


func (this *Common) StrToMapSet(str string,sep string) mapset.Set {
	result:=mapset.NewSet()
	for _,v:=range strings.Split(str,sep) {
		result.Add(v)
	}
	return result
}

func (this *Common) MapSetToStr(set mapset.Set,sep string) string{

	var (
		ret []string
	)

	for v:=range  set.Iter() {
		ret=append(ret,v.(string))
	}
	return strings.Join(ret,sep)

}

func (this *Common) GetPulicIP() string {
	conn, _ := net.Dial("udp", "8.8.8.8:80")
	defer conn.Close()
	localAddr := conn.LocalAddr().String()
	idx := strings.LastIndex(localAddr, ":")
	return localAddr[0:idx]
}

func (this *Common) MD5(str string) string {

	md := md5.New()
	md.Write([]byte(str))
	return fmt.Sprintf("%x", md.Sum(nil))
}

func (this *Common) GetFileMd5(file *os.File) string {
	file.Seek(0, 0)
	md5h := md5.New()
	io.Copy(md5h, file)
	sum := fmt.Sprintf("%x", md5h.Sum(nil))
	return sum
}

func (this *Common) Contains(obj interface{}, arrayobj interface{}) bool {
	targetValue := reflect.ValueOf(arrayobj)
	switch reflect.TypeOf(arrayobj).Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < targetValue.Len(); i++ {
			if targetValue.Index(i).Interface() == obj {
				return true
			}
		}
	case reflect.Map:
		if targetValue.MapIndex(reflect.ValueOf(obj)).IsValid() {
			return true
		}
	}
	return false
}

func (this *Common) FileExists(fileName string) bool {
	_, err := os.Stat(fileName)
	return err == nil
}

func (this *Common) WriteFile(path string, data string) bool {
	if err := ioutil.WriteFile(path, []byte(data), 0666); err == nil {
		return true
	} else {
		return false
	}
}

func (this *Common) WriteBinFile(path string, data []byte) bool {
	if err := ioutil.WriteFile(path, data, 0666); err == nil {
		return true
	} else {
		return false
	}
}

func (this *Common) IsExist(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil || os.IsExist(err)
}

func (this *Common) Match(matcher string, content string) []string {
	var result []string
	if reg, err := regexp.Compile(matcher); err == nil {

		result = reg.FindAllString(content, -1)

	}
	return result
}

func (this *Common) ReadBinFile(path string) ([]byte, error) {
	if this.IsExist(path) {
		fi, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer fi.Close()
		return ioutil.ReadAll(fi)
	} else {
		return nil, errors.New("not found")
	}
}
func (this *Common) RemoveEmptyDir(pathname string) {
	defer func() {
		if re := recover(); re != nil {
			buffer := debug.Stack()
			log.Error("postFileToPeer")
			log.Error(re)
			log.Error(string(buffer))
		}
	}()

	handlefunc := func(file_path string, f os.FileInfo, err error) error {

		if f.IsDir() {

			files, _ := ioutil.ReadDir(file_path)
			if len(files) == 0 && file_path != pathname {
				os.Remove(file_path)
			}

		}

		return nil
	}

	fi, _ := os.Stat(pathname)
	if fi.IsDir() {
		filepath.Walk(pathname, handlefunc)
	}

}

func (this *Common) JsonEncodePretty(o interface{}) string {

	resp := ""
	switch o.(type) {
	case map[string]interface{}:
		if data, err := json.Marshal(o); err == nil {
			resp = string(data)
		}
	case map[string]string:
		if data, err := json.Marshal(o); err == nil {
			resp = string(data)
		}
	case []interface{}:
		if data, err := json.Marshal(o); err == nil {
			resp = string(data)
		}
	case []string:
		if data, err := json.Marshal(o); err == nil {
			resp = string(data)
		}
	case string:
		resp = o.(string)

	default:
		if data, err := json.Marshal(o); err == nil {
			resp = string(data)
		}

	}
	var v interface{}
	if ok := json.Unmarshal([]byte(resp), &v); ok == nil {
		if buf, ok := json.MarshalIndent(v, "", "  "); ok == nil {
			resp = string(buf)
		}
	}
	return resp

}

func (this *Common) GetClientIp(r *http.Request) string {

	client_ip := ""
	headers := []string{"X_Forwarded_For", "X-Forwarded-For", "X-Real-Ip",
		"X_Real_Ip", "Remote_Addr", "Remote-Addr"}
	for _, v := range headers {
		if _v, ok := r.Header[v]; ok {
			if len(_v) > 0 {
				client_ip = _v[0]
				break
			}
		}
	}
	if client_ip == "" {
		clients := strings.Split(r.RemoteAddr, ":")
		client_ip = clients[0]
	}
	return client_ip

}

func (this *Server) RepairStat() {

	defer func() {
		if re := recover(); re != nil {
			buffer := debug.Stack()
			log.Error("RepairStat")
			log.Error(re)
			log.Error(string(buffer))
			fmt.Println(re)
		}
	}()

	this.statMap.Put(CONST_STAT_FILE_COUNT_KEY, int64(0))
	this.statMap.Put(CONST_STAT_FILE_TOTAL_SIZE_KEY, int64(0))


	handlefunc := func(file_path string, f os.FileInfo, err error) error {

		var (
			files     []os.FileInfo
			date      []string
			data      []byte
			content   string
			lines     []string
			count     int64
			totalSize int64
			line      string
			cols      []string
			size      int64
		)

		if f.IsDir() {

			if files, err = ioutil.ReadDir(file_path); err != nil {
				return err
			}

			for _, file := range files {
				count = 0
				size = 0
				if file.Name() == CONST_FILE_Md5_FILE_NAME {
					if data, err = ioutil.ReadFile(file_path + "/" + file.Name()); err != nil {
						log.Error(err)
						continue
					}
					date = this.util.Match("\\d{8}", file_path)
					if len(date) < 1 {
						continue
					}
					content = string(data)
					lines = strings.Split(content, "\n")
					count = int64(len(lines))
					if count > 1 {
						count = count - 1
					}
				    count=0
					for _, line = range lines {

						cols = strings.Split(line, "|")
						if len(cols) > 2 {
							count=count+1
							if size, err = strconv.ParseInt(cols[1], 10, 64); err != nil {
								size = 0
								continue
							}
							totalSize = totalSize + size
						}

					}
					this.statMap.Put(date[0]+"_"+CONST_STAT_FILE_COUNT_KEY, count)
					this.statMap.Put(date[0]+"_"+CONST_STAT_FILE_TOTAL_SIZE_KEY, totalSize)
					this.statMap.AddCountInt64(CONST_STAT_FILE_COUNT_KEY, count)
					this.statMap.AddCountInt64(CONST_STAT_FILE_TOTAL_SIZE_KEY, totalSize)

				}

			}

		}

		return nil
	}



	filepath.Walk(DATA_DIR, handlefunc)


	this.SaveStat()

}





func (this *Server) DownloadFromPeer(peer string, fileInfo *FileInfo) {
	var (
		err      error
		filename string
		fpath    string
		fi       os.FileInfo
	)
	if _, err = os.Stat(fileInfo.Path); err != nil {
		os.MkdirAll(fileInfo.Path, 0666)
	}

	filename = fileInfo.Name
	if fileInfo.ReName != "" {
		filename = fileInfo.ReName
	}

	p := strings.Replace(fileInfo.Path, STORE_DIR+"/", "", 1)
	req := httplib.Get(peer + "/" + Config().Group + "/" + p + "/" + filename)

	fpath = fileInfo.Path + "/" + filename



	req.SetTimeout(time.Second*5, time.Second*5)




	if err = req.ToFile(fpath); err != nil {
		log.Error(err)
	}

	if fi, err = os.Stat(fpath); err != nil {
		os.Remove(fpath)
		return
	}
	if fi.Size() == 0 {
		os.Remove(fpath)
	}

}

func (this *Server) Download(w http.ResponseWriter, r *http.Request) {

	var (
		err          error
		pathMd5      string
		info         os.FileInfo
		peer         string
		fileInfo     *FileInfo
		fullpath     string
		pathval      url.Values
		token        string
		timestamp    string
		maxTimestamp int64
		minTimestamp int64
		ts           int64
		md5sum       string
		fp           *os.File
		isPeer bool
	)

	r.ParseForm()


	isPeer=this.IsPeer(r)



	if Config().DownloadUseToken &&!isPeer {

		token = r.FormValue("token")
		timestamp = r.FormValue("timestamp")

		if token == "" || timestamp == "" {
			w.Write([]byte("unvalid request"))
			return
		}

		maxTimestamp = time.Now().Add(time.Second *
			time.Duration(Config().DownloadTokenExpire)).Unix()
		minTimestamp = time.Now().Add(-time.Second *
			time.Duration(Config().DownloadTokenExpire)).Unix()
		if ts, err = strconv.ParseInt(timestamp, 10, 64); err != nil {
			w.Write([]byte("unvalid timestamp"))
			return
		}

		if ts > maxTimestamp || ts < minTimestamp {
			w.Write([]byte("timestamp expire"))
			return
		}

	}

	fullpath = r.RequestURI[len(Config().Group)+2 : len(r.RequestURI)]

	fullpath = STORE_DIR + "/" + fullpath

	if pathval, err = url.ParseQuery(fullpath); err != nil {
		log.Error(err)
	} else {

		for k, _ := range pathval {
			if k != "" {
				fullpath = k
				break
			}
		}

	}

	CheckToken := func(token string, md5sum string, timestamp string) bool {
		if this.util.MD5(md5sum+timestamp) != token {
			return false
		}
		return true
	}

	if Config().DownloadUseToken  &&!isPeer {
		fullpath = strings.Split(fullpath, "?")[0]
		pathMd5 = this.util.MD5(fullpath)
		if fileInfo, err = this.GetFileInfoFromLevelDB(pathMd5); err != nil {
			log.Error(err)
			if this.util.FileExists(fullpath) {
				if fp, err = os.Create(fullpath); err != nil {
					log.Error(err)
				}
				if fp != nil {
					defer fp.Close()
				}
				md5sum = this.util.GetFileMd5(fp)
				if !CheckToken(token, md5sum, timestamp) {
					w.Write([]byte("unvalid request,error token"))
					return
				}
			}
		} else {
			if !CheckToken(token, fileInfo.Md5, timestamp) {
				w.Write([]byte("unvalid request,error token"))
				return
			}
		}

	}

	if info, err = os.Stat(fullpath); err != nil {
		log.Error(err)
		pathMd5 = this.util.MD5(fullpath)
		for _, peer = range Config().Peers {

			if fileInfo, err = this.checkPeerFileExist(peer, pathMd5); err != nil {
				log.Error(err)
				continue
			}

			if fileInfo.Md5 != "" {

				if Config().DownloadUseToken &&!isPeer {
					if !CheckToken(token, fileInfo.Md5, timestamp) {
						w.Write([]byte("unvalid request,error token"))
						return
					}
				}

				go this.DownloadFromPeer(peer, fileInfo)

				http.Redirect(w, r, peer+r.RequestURI, 302)
				return
			}

		}
		w.WriteHeader(404)
		return
	}

	if !Config().ShowDir && info.IsDir() {
		w.Write([]byte("list dir deny"))
		return
	}

	log.Info("download:" + r.RequestURI)
	staticHandler.ServeHTTP(w, r)
}

func (this *Server) GetServerURI(r *http.Request) string {
	return fmt.Sprintf("http://%s/", r.Host)
}

func (this *Server) CheckFileAndSendToPeer(filename string, is_force_upload bool) {

	defer func() {
		if re := recover(); re != nil {
			buffer := debug.Stack()
			log.Error("CheckFileAndSendToPeer")
			log.Error(re)
			log.Error(string(buffer))
		}
	}()

	if filename == "" {
		filename = DATA_DIR + "/" + time.Now().Format("20060102") + "/" + CONST_Md5_ERROR_FILE_NAME
	}

	if data, err := ioutil.ReadFile(filename); err == nil {
		content := string(data)

		lines := strings.Split(content, "\n")
		for _, line := range lines {
			cols := strings.Split(line, "|")

			if fileInfo, _ := this.GetFileInfoByMd5(cols[0]); fileInfo != nil && fileInfo.Md5 != "" {
				if is_force_upload {
					fileInfo.Peers = []string{}
				}
				//this.postFileToPeer(fileInfo, false)
				fileInfo.writeLog=false
				this.queueToPeers<-*fileInfo
			}
		}

	}

}

func (this *Server) postFileToPeer(fileInfo *FileInfo) {

	var (
		err      error
		peer     string
		filename string
		info     *FileInfo
		postURL  string
		result   string
		data     []byte
		fi       os.FileInfo
		i        int
	)

	defer func() {
		if re := recover(); re != nil {
			buffer := debug.Stack()
			log.Error("postFileToPeer")
			log.Error(re)
			log.Error(string(buffer))
		}
	}()

	for i, peer = range Config().Peers {

		_ = i

		if fileInfo.Peers == nil {
			fileInfo.Peers = []string{}
		}
		if this.util.Contains(peer, fileInfo.Peers) {
			continue
		}

		filename = fileInfo.Name

		if Config().RenameFile {
			filename = fileInfo.ReName
		}
		if !this.util.FileExists(fileInfo.Path + "/" + filename) {
			continue
		} else {
			if fileInfo.Size == 0 {
				if fi, err = os.Stat(fileInfo.Path + "/" + filename); err != nil {
					log.Error(err)
				} else {
					fileInfo.Size = fi.Size()
				}
			}
		}

		if info, err = this.checkPeerFileExist(peer, fileInfo.Md5); info.Md5 != "" {

			continue
		}







		postURL = fmt.Sprintf("%s/%s", peer, "syncfile")
		b := httplib.Post(postURL)



		b.SetTimeout(time.Second*1, time.Second*1)
		b.Header("Sync-Path", fileInfo.Path)
		b.Param("name", filename)
		b.Param("md5", fileInfo.Md5)
		b.Param("timestamp", fmt.Sprintf("%d", fileInfo.TimeStamp))
		b.PostFile("file", fileInfo.Path+"/"+filename)
		b.Debug(true) //fuck this is a bug
		result, err = b.String()

		if !strings.HasPrefix(result, "http://") {
			if fileInfo.writeLog {
				this.SaveFileMd5Log(fileInfo, CONST_Md5_ERROR_FILE_NAME)
			}
		} else {

			log.Info(result)

			if !this.util.Contains(peer, fileInfo.Peers) {
				fileInfo.Peers = append(fileInfo.Peers, peer)
				if data, err = json.Marshal(fileInfo); err != nil {
					log.Error(err)
					return
				}
				this.ldb.Put([]byte(fileInfo.Md5), data, nil)
			}

		}
		if err != nil {
			log.Error(err)
		}

	}

}

func (this *Server) SaveFileMd5Log(fileInfo *FileInfo, filename string) {
	var (
		err     error
		msg     string
		tmpFile *os.File

		logpath string
		outname string
	)

	outname = fileInfo.Name

	if this.curDate!=this.util.GetToDay() {
		this.errorset.Clear()
		this.fileset.Clear()
	}

	if fileInfo.ReName != "" {
		outname = fileInfo.ReName
	}

	if filename==CONST_Md5_ERROR_FILE_NAME && this.errorset.Contains(fileInfo.Md5) {
		return
	}

	if filename==CONST_FILE_Md5_FILE_NAME && this.fileset.Contains(fileInfo.Md5) {
		return
	}

	logpath = DATA_DIR + "/" + time.Unix(fileInfo.TimeStamp, 0).Format("20060102")
	if _, err = os.Stat(logpath); err != nil {
		os.MkdirAll(logpath, 0666)
	}
	msg = fmt.Sprintf("%s|%d|%s\n", fileInfo.Md5, fileInfo.Size, fileInfo.Path+"/"+outname)
	if tmpFile, err = os.OpenFile(logpath+"/"+filename, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644); err != nil {
		log.Error(err)
		return
	}
	defer tmpFile.Close()
	tmpFile.WriteString(msg)
	if filename==CONST_FILE_Md5_FILE_NAME {
		this.fileset.Add(fileInfo.Md5)
	}
	if filename==CONST_Md5_ERROR_FILE_NAME {
		this.errorset.Add(fileInfo.Md5)
	}

}

func (this *Server) GetFileInfoByMd5(md5sum string) (*FileInfo, error) {
	var (
		data     []byte
		err      error
		fileInfo FileInfo
	)

	if data, err = this.ldb.Get([]byte(md5sum), nil); err != nil {
		return nil, err
	} else {
		if err = json.Unmarshal(data, &fileInfo); err == nil {
			return &fileInfo, nil
		} else {
			return nil, err
		}
	}
}

func (this *Server) checkPeerFileExist(peer string, md5sum string) (*FileInfo, error) {

	var (
		err      error
		fileInfo FileInfo
	)

	req := httplib.Get(peer + fmt.Sprintf("/check_file_exist?md5=%s", md5sum))

	req.SetTimeout(time.Second*5, time.Second*5)

	if err = req.ToJSON(&fileInfo); err != nil {
		return &FileInfo{}, err
	}

	if fileInfo.Md5 == "" {
		return &fileInfo, errors.New("not found")
	}

	return &fileInfo, nil

}

func (this *Server) CheckFileExist(w http.ResponseWriter, r *http.Request) {
	var (
		data     []byte
		err      error
		fileInfo *FileInfo
	)
	r.ParseForm()
	md5sum := ""
	if len(r.Form["md5"]) > 0 {
		md5sum = r.Form["md5"][0]
	} else {
		return
	}

	if fileInfo, err = this.GetFileInfoByMd5(md5sum); fileInfo != nil {

		if data, err = json.Marshal(fileInfo); err == nil {
			w.Write(data)
			return
		}
	}

	data, _ = json.Marshal(FileInfo{})

	w.Write(data)

}

func (this *Server) Sync(w http.ResponseWriter, r *http.Request) {

	var (
		filename string

	)


	r.ParseForm()


	if !this.IsPeer(r) {
		w.Write([]byte("client must be in cluster"))
		return
	}

	date := ""

	force := ""
	is_force_upload := false

	force=r.FormValue("force")
	date=r.FormValue("date")

	if force == "1" {
		is_force_upload = true
	}

	if date=="" {

		w.Write([]byte("require paramete date &force , date?=20181230"))
		return
	}
	date = strings.Replace(date, ".", "", -1)

	if is_force_upload {

		filename = DATA_DIR + "/" + date + "/" + CONST_FILE_Md5_FILE_NAME

		if this.util.FileExists(filename) {

			go this.CheckFileAndSendToPeer(filename, is_force_upload)
		} else {
			w.Write([]byte(fmt.Sprintf( "%s not found",filename)))
			return
		}
	}  else {

		filename = DATA_DIR + "/" + date + "/" + CONST_Md5_ERROR_FILE_NAME

		if this.util.FileExists(filename) {

			go this.CheckFileAndSendToPeer(filename, is_force_upload)
		} else {
			w.Write([]byte(fmt.Sprintf( "%s not found",filename)))
			return
		}


	}

	//for _,peer:=range Config().Peers {
	//
	//	fmt.Println(peer)
	//
	//	req:=httplib.Post(fmt.Sprintf("%s/sync",peer))
	//	req.Param("date",date)
	//	if is_force_upload {
	//		req.Param("force","1")
	//	}
	//	if _,err:=req.String();err!=nil {
	//		log.Error(err)
	//	}
	//
	//
	//
	//}

	w.Write([]byte("job is running"))
}

func (this *Server) GetFileInfoFromLevelDB(key string) (*FileInfo, error) {
	var (
		err  error
		data []byte

		fileInfo FileInfo
	)

	if data, err = this.ldb.Get([]byte(key), nil); err != nil {
		return nil, err
	}

	if err = json.Unmarshal(data, &fileInfo); err != nil {
		return nil, err
	}
	return &fileInfo, nil

}

func (this *Server) SaveStat() {

	SaveStatFunc := func() {

		defer func() {
			if re := recover(); re != nil {
				buffer := debug.Stack()
				log.Error("SaveStatFunc")
				log.Error(re)
				log.Error(string(buffer))
			}
		}()

		stat := this.statMap.Get()
		if v, ok := stat[CONST_STAT_FILE_TOTAL_SIZE_KEY]; ok {
			switch v.(type) {
			case int64:
				if v.(int64) >=0 {

					if data, err := json.Marshal(stat); err != nil {
						log.Error(err)
					} else {
						this.util.WriteBinFile(CONST_STAT_FILE_NAME, data)
					}

				}
			}

		}

	}

	SaveStatFunc()
	//
	//for {
	//
	//	time.Sleep(time.Minute * 1)
	//	SaveStatFunc()
	//}

}

func (this *Server) SaveFileInfoToLevelDB(key string, fileInfo *FileInfo) (*FileInfo, error) {
	var (
		err  error
		data []byte
	)

	if data, err = json.Marshal(fileInfo); err != nil {

		return fileInfo, err

	}

	if err = this.ldb.Put([]byte(key), data, nil); err != nil {
		return fileInfo, err
	}

	return fileInfo, nil

}

func (this *Server) IsPeer(r *http.Request) bool {
	var (
		ip    string
		peer  string
		bflag bool
	)
	ip = this.util.GetClientIp(r)

	if ip=="127.0.0.1" || ip==this.util.GetPulicIP() {
		return true
	}
	ip = "http://" + ip
	bflag = false




	for _, peer = range Config().Peers {
		if strings.HasPrefix(peer, ip) {
			bflag = true
			break
		}
	}
	return bflag
}

func (this *Server) ReceiveMd5s(w http.ResponseWriter, r *http.Request) {

	var (
		err error
		md5s string
		mdfs mapset.Set
		fileInfo *FileInfo

	)

	if !this.IsPeer(r) {
		log.Warn(fmt.Sprintf("ReceiveMd5s %s",this.util.GetClientIp(r)))
		return
	}

	mdfs=mapset.NewSet()

	r.ParseForm()
	md5s=r.FormValue("md5s")
	for _,v:= range strings.Split(md5s,",") {

		mdfs.Add(v)

	}


	for m:=range mdfs.Iter() {
	  if fileInfo,err	=this.GetFileInfoByMd5(m.(string));err!=nil {
	  	log.Error(err)
	  	 continue
	  }
	  this.queueToPeers<- *fileInfo
	}


}

func (this *Server) GetMd5sForWeb(w http.ResponseWriter, r *http.Request) {

	var (
		date string
		err error
		result mapset.Set
		lines []string
	)

	if !this.IsPeer(r) {

		return

	}
	 date = r.FormValue("date")

	  if result,err=this.GetMd5sByDate(date);err!=nil {
	  	log.Error(err)
		  return
	  }
     for line:=range result.Iter() {
     	lines=append(lines,line.(string))
	 }
	w.Write([]byte( strings.Join(lines,",") ))


}

func (this *Server) GetMd5File(w http.ResponseWriter, r *http.Request) {

	var (
		date string
		fpath string
		data []byte
		err error
	)
	if !this.IsPeer(r) {

		return

	}

	fpath=DATA_DIR+"/"+date+"/"+CONST_FILE_Md5_FILE_NAME

	if !this.util.FileExists(fpath) {
		w.WriteHeader(404)
		return
	}
	if data,err=ioutil.ReadFile(fpath);err!=nil {
		w.WriteHeader(500)
		return
	}
	w.Write(data)

}


func (this *Server) GetMd5sByDate(date string) (mapset.Set,error) {

	var (
		err error
		result mapset.Set
		fpath string
		content string
		lines []string
		line string
		cols []string
		data []byte
	)

	 result=mapset.NewSet()

	fpath=DATA_DIR+"/"+date+"/"+CONST_FILE_Md5_FILE_NAME

	if !this.util.FileExists(fpath) {
		return result,errors.New("not found")
	}

	if data,err=ioutil.ReadFile(fpath);err!=nil {
		return result,err
	}
	content=string(data)
	lines=strings.Split(content,"\n")
	for _, line = range lines {

		cols = strings.Split(line, "|")
		if len(cols) > 2 {
			if _, err = strconv.ParseInt(cols[1], 10, 64); err != nil {
				continue
			}
			result.Add(cols[0])

		}
	}
   return result,nil
}






func (this *Server) SyncFile(w http.ResponseWriter, r *http.Request) {
	var (
		err     error
		outPath string
		//outname string
		// timestamp  string
		fileInfo   FileInfo
		tmpFile    *os.File
		fi         os.FileInfo
		uploadFile multipart.File
	)

	if !this.IsPeer(r) {
		log.Error(fmt.Sprintf(" not is peer,ip:%s", this.util.GetClientIp(r)))
		return
	}

	if r.Method == "POST" {

		fileInfo.Path = r.Header.Get("Sync-Path")
		fileInfo.Md5 = r.PostFormValue("md5")
		fileInfo.Name = r.PostFormValue("name")
		fileInfo.TimeStamp, err = strconv.ParseInt(r.PostFormValue("timestamp"), 10, 64)
		if err != nil {
			fileInfo.TimeStamp = time.Now().Unix()
			log.Error(err)
		}
		if uploadFile, _, err = r.FormFile("file"); err != nil {
			w.Write([]byte(err.Error()))
			log.Error(err)
			return
		}
		fileInfo.Peers = []string{}

		defer uploadFile.Close()

		//if v, _ := this.GetFileInfoFromLevelDB(fileInfo.Md5); v != nil && v.Md5 != "" {
		//	outname = v.Name
		//	if v.ReName != "" {
		//		outname = v.ReName
		//	}
		//
		//	p := strings.Replace(v.Path, STORE_DIR+"/", "", 1)
		//
		//	download_url := fmt.Sprintf("http://%s/%s", r.Host, Config().Group+"/"+p+"/"+outname)
		//	w.Write([]byte(download_url))
		//
		//	return
		//}

		os.MkdirAll(fileInfo.Path, 0666)

		outPath = fileInfo.Path + "/" + fileInfo.Name

		if this.util.FileExists(outPath) {
			if tmpFile, err = os.Open(outPath); err != nil {
				log.Error(err)
				w.Write([]byte(err.Error()))
				return
			}
			if this.util.GetFileMd5(tmpFile) != fileInfo.Md5 {
				tmpFile.Close()
				log.Error("md5 !=fileInfo.Md5 ")
				w.Write([]byte("md5 !=fileInfo.Md5 "))
				return
			}
		}

		if tmpFile, err = os.Create(outPath); err != nil {
			log.Error(err)
			w.Write([]byte(err.Error()))
			return
		}

		defer tmpFile.Close()

		if _, err = io.Copy(tmpFile, uploadFile); err != nil {
			w.Write([]byte(err.Error()))
			log.Error(err)

			return
		}
		if this.util.GetFileMd5(tmpFile) != fileInfo.Md5 {
			w.Write([]byte("md5 error"))
			tmpFile.Close()
			os.Remove(outPath)

			return

		}
		if fi, err = os.Stat(outPath); err != nil {
			log.Error(err)
		} else {
			fileInfo.Size = fi.Size()
			date:=time.Unix(fileInfo.TimeStamp,0).Format("20060102")
			this.statMap.AddCountInt64(date+"_"+CONST_STAT_FILE_COUNT_KEY, 1)
			this.statMap.AddCountInt64(date+"_"+CONST_STAT_FILE_TOTAL_SIZE_KEY, fi.Size())
			this.statMap.AddCountInt64(CONST_STAT_FILE_TOTAL_SIZE_KEY, fi.Size())
			this.statMap.AddCountInt64(CONST_STAT_FILE_COUNT_KEY, 1)

		}

		if fileInfo.Peers == nil {
			fileInfo.Peers = []string{fmt.Sprintf("http://%s", r.Host)}
		}
		if _, err = this.SaveFileInfoToLevelDB(this.util.MD5(outPath), &fileInfo); err != nil {
			log.Error(err)
		}
		if _, err = this.SaveFileInfoToLevelDB(fileInfo.Md5, &fileInfo); err != nil {
			log.Error(err)
		}

		this.SaveFileMd5Log(&fileInfo, CONST_FILE_Md5_FILE_NAME)

		p := strings.Replace(fileInfo.Path, STORE_DIR+"/", "", 1)

		download_url := fmt.Sprintf("http://%s/%s", r.Host, Config().Group+"/"+p+"/"+fileInfo.Name)
		w.Write([]byte(download_url))

	}

}

func (this *Server) CheckScene(scene string) (bool, error) {

	if len(Config().Scenes) == 0 {
		return true, nil
	}

	if !this.util.Contains(scene, Config().Scenes) {
		return false, errors.New("not valid scene")
	}
	return true, nil

}

func (this *Server) RemoveFile(w http.ResponseWriter, r *http.Request) {
	var (
		err      error
		md5sum   string
		fileInfo *FileInfo
		fpath    string
	)

	r.ParseForm()

	md5sum = r.FormValue("md5")

	if len(md5sum) != 32 {
		w.Write([]byte("md5 unvalid"))
		return
	}
	if fileInfo, err = this.GetFileInfoByMd5(md5sum); err != nil {
		w.Write([]byte(err.Error()))
		return
	}

	if fileInfo.ReName != "" {
		fpath = fileInfo.Path + "/" + fileInfo.ReName
	} else {
		fpath = fileInfo.Path + "/" + fileInfo.Name
	}

	if fileInfo.Path != "" && this.util.FileExists(fpath) {
		if err = os.Remove(fpath); err != nil {
			w.Write([]byte(err.Error()))
			return
		} else {
			w.Write([]byte("remove success"))
			return
		}
	}
	w.Write([]byte("fail remove"))

}

func (this *Server) Upload(w http.ResponseWriter, r *http.Request) {

	var (
		err error

		//		pathname     string
		outname      string
		md5sum       string
		fileInfo     FileInfo
		uploadFile   multipart.File
		uploadHeader *multipart.FileHeader
		scene        string
		output       string
		fileResult   FileResult
		data         []byte
		domain       string
	)
	if r.Method == "POST" {
		//		name := r.PostFormValue("name")

		//		fileInfo.Path = r.Header.Get("Sync-Path")

		if Config().EnableCustomPath {
			fileInfo.Path = r.FormValue("path")
			fileInfo.Path = strings.Trim(fileInfo.Path, "/")
		}
		scene = r.FormValue("scene")
		if scene == "" {
			//Just for Compatibility
			scene = r.FormValue("scenes")
		}
		md5sum = r.FormValue("md5")
		output = r.FormValue("output")

		fileInfo.Md5 = md5sum
		if uploadFile, uploadHeader, err = r.FormFile("file"); err != nil {
			w.Write([]byte(err.Error()))
			return
		}
		fileInfo.Peers = []string{}
		fileInfo.TimeStamp = time.Now().Unix()

		if scene == "" {
			scene = Config().DefaultScene
		}

		if output == "" {
			output = "text"
		}

		if !this.util.Contains(output, []string{"json", "text"}) {
			w.Write([]byte("output just support json or text"))
			return
		}

		fileInfo.Scene = scene

		if _, err = this.CheckScene(scene); err != nil {

			w.Write([]byte(err.Error()))

			return
		}

		if Config().DownloadDomain != "" {
			domain = fmt.Sprintf("http://%s", Config().DownloadDomain)
		} else {
			domain = fmt.Sprintf("http://%s", r.Host)
		}

		if err != nil {
			log.Error(err)
			fmt.Printf("FromFileErr")
			http.Redirect(w, r, "/", http.StatusMovedPermanently)
			return
		}

		SaveUploadFile := func(file multipart.File, header *multipart.FileHeader, fileInfo *FileInfo) (*FileInfo, error) {
			var (
				err     error
				outFile *os.File
				folder  string
			)

			defer file.Close()

			fileInfo.Name = header.Filename

			if Config().RenameFile {
				fileInfo.ReName = this.util.MD5(this.util.GetUUID()) + path.Ext(fileInfo.Name)
			}

			folder = time.Now().Format("20060102/15/04")
			if fileInfo.Scene != "" {
				folder = fmt.Sprintf(STORE_DIR+"/%s/%s", fileInfo.Scene, folder)
			} else {
				folder = fmt.Sprintf(STORE_DIR+"/%s", folder)
			}
			if fileInfo.Path != "" {
				if strings.HasPrefix(fileInfo.Path, STORE_DIR) {
					folder = fileInfo.Path
				} else {

					folder = STORE_DIR + "/" + fileInfo.Path
				}
			}

			if !this.util.FileExists(folder) {
				os.MkdirAll(folder, 0666)
			}

			outPath := fmt.Sprintf(folder+"/%s", fileInfo.Name)
			if Config().RenameFile {
				outPath = fmt.Sprintf(folder+"/%s", fileInfo.ReName)
			}

			if this.util.FileExists(outPath) {
				for i := 0; i < 10000; i++ {
					outPath = fmt.Sprintf(folder+"/%d_%s", i, header.Filename)
					fileInfo.Name = fmt.Sprintf("%d_%s", i, header.Filename)
					if !this.util.FileExists(outPath) {
						break
					}
				}
			}

			log.Info(fmt.Sprintf("upload: %s", outPath))

			if outFile, err = os.Create(outPath); err != nil {
				return fileInfo, err
			}

			defer outFile.Close()

			if err != nil {
				log.Error(err)
				return fileInfo, errors.New("(error)fail," + err.Error())

			}

			if _, err = io.Copy(outFile, file); err != nil {
				log.Error(err)
				return fileInfo, errors.New("(error)fail," + err.Error())
			}

			v := this.util.GetFileMd5(outFile)
			fileInfo.Md5 = v
			fileInfo.Path = folder

			fileInfo.Peers = append(fileInfo.Peers, fmt.Sprintf("http://%s", r.Host))

			return fileInfo, nil

		}

		SaveUploadFile(uploadFile, uploadHeader, &fileInfo)

		if v, _ := this.GetFileInfoFromLevelDB(fileInfo.Md5); v != nil && v.Md5 != "" {

			if Config().RenameFile {
				os.Remove(fileInfo.Path + "/" + fileInfo.ReName)
			} else {
				os.Remove(fileInfo.Path + "/" + fileInfo.Name)
			}
			outname = v.Name
			if v.ReName != "" {
				outname = v.ReName
			}
			p := strings.Replace(v.Path, STORE_DIR+"/", "", 1)
			p = Config().Group + "/" + p + "/" + outname
			download_url := fmt.Sprintf("http://%s/%s", r.Host, p)
			if Config().DownloadDomain != "" {
				download_url = fmt.Sprintf("http://%s/%s", Config().DownloadDomain, p)
			}
			if output == "json" {
				fileResult.Url = download_url
				fileResult.Md5 = v.Md5
				fileResult.Path = "/" + p
				fileResult.Domain = domain
				fileResult.Scene = fileInfo.Scene

				// Just for Compatibility
				fileResult.Src = fileResult.Path
				fileResult.Scenes = fileInfo.Scene

				if data, err = json.Marshal(fileResult); err != nil {
					w.Write([]byte(err.Error()))
					return
				}
				w.Write(data)

			} else {

				w.Write([]byte(download_url))
			}
			return
		}

		if fileInfo.Md5 == "" {
			log.Warn(" fileInfo.Md5 is null")
			return
		}

		if md5sum != "" && fileInfo.Md5 != md5sum {
			log.Warn(" fileInfo.Md5 and md5sum !=")
			return
		}

		UploadToPeer := func(fileInfo *FileInfo) {

			var (
				err      error
				pathMd5  string
				fullpath string
				//				data     []byte
			)

			if _, err = json.Marshal(fileInfo); err != nil {
				log.Error(err)
				log.Error(fmt.Sprintf("UploadToPeer fail: %v", fileInfo))
				return

			}

			if _, err = this.SaveFileInfoToLevelDB(fileInfo.Md5, fileInfo); err != nil {
				log.Error("SaveFileInfoToLevelDB fail", err)

			}

			fullpath = fileInfo.Path + "/" + fileInfo.Name

			if Config().RenameFile {
				fullpath = fileInfo.Path + "/" + fileInfo.ReName
			}

			pathMd5 = this.util.MD5(fullpath)

			if _, err = this.SaveFileInfoToLevelDB(pathMd5, fileInfo); err != nil {
				log.Error("SaveFileInfoToLevelDB fail", err)
			}

			if len(this.queueToPeers) < CONST_QUEUE_SIZE {
				//this.queueToPeers <- FileInfo{Name: fileInfo.Name,
				//	Peers: []string{}, TimeStamp: fileInfo.TimeStamp,
				//	Path: fileInfo.Path, Md5: fileInfo.Md5, ReName: fileInfo.ReName,
				//	Size: fileInfo.Size, Scene: fileInfo.Scene}

				fileInfo.writeLog=true
				this.queueToPeers<-*fileInfo
			}

			// go this.postFileToPeer(fileInfo, true)

		}

		UploadToPeer(&fileInfo)

		outname = fileInfo.Name

		if Config().RenameFile {
			outname = fileInfo.ReName
		}

		if fi, err := os.Stat(fileInfo.Path + "/" + outname); err != nil {
			log.Error(err)
		} else {
			fileInfo.Size = fi.Size()
			this.statMap.AddCountInt64(CONST_STAT_FILE_TOTAL_SIZE_KEY, fi.Size())
			this.statMap.AddCountInt64(CONST_STAT_FILE_COUNT_KEY, 1)
			this.statMap.AddCountInt64(this.util.GetToDay()+"_"+CONST_STAT_FILE_TOTAL_SIZE_KEY, fi.Size())
			this.statMap.AddCountInt64(this.util.GetToDay()+"_"+CONST_STAT_FILE_COUNT_KEY, 1)
		}

		this.SaveFileMd5Log(&fileInfo, CONST_FILE_Md5_FILE_NAME)

		this.SaveStat()

		p := strings.Replace(fileInfo.Path, STORE_DIR+"/", "", 1)
		p = Config().Group + "/" + p + "/" + outname
		download_url := fmt.Sprintf("http://%s/%s", r.Host, p)
		if Config().DownloadDomain != "" {
			download_url = fmt.Sprintf("http://%s/%s", Config().DownloadDomain, p)
		}

		if output == "json" {
			fileResult.Url = download_url
			fileResult.Md5 = fileInfo.Md5
			fileResult.Path = "/" + p
			fileResult.Domain = domain
			fileResult.Scene = fileInfo.Scene
			// Just for Compatibility
			fileResult.Src = fileResult.Path
			fileResult.Scenes = fileInfo.Scene

			if data, err = json.Marshal(fileResult); err != nil {
				w.Write([]byte(err.Error()))
				return
			}
			w.Write(data)

		} else {

			w.Write([]byte(download_url))
		}
		return

	} else {
		w.Write([]byte("(error)fail,please use post method"))
		return
	}

}

func (this *Server) SendToMail(to, subject, body, mailtype string) error {
	host := Config().Mail.Host
	user := Config().Mail.User
	password := Config().Mail.Password
	hp := strings.Split(host, ":")
	auth := smtp.PlainAuth("", user, password, hp[0])
	var content_type string
	if mailtype == "html" {
		content_type = "Content-Type: text/" + mailtype + "; charset=UTF-8"
	} else {
		content_type = "Content-Type: text/plain" + "; charset=UTF-8"
	}

	msg := []byte("To: " + to + "\r\nFrom: " + user + ">\r\nSubject: " + "\r\n" + content_type + "\r\n\r\n" + body)
	send_to := strings.Split(to, ";")
	err := smtp.SendMail(host, auth, user, send_to, msg)
	return err
}

func (this *Server) BenchMark(w http.ResponseWriter, r *http.Request) {
	t := time.Now()
	batch := new(leveldb.Batch)

	for i := 0; i < 100000000; i++ {
		f := FileInfo{}
		f.Peers = []string{"http://192.168.0.1", "http://192.168.2.5"}
		f.Path = "20190201/19/02"
		s := strconv.Itoa(i)
		s = this.util.MD5(s)
		f.Name = s
		f.Md5 = s

		//		server.SaveFileInfoToLevelDB(s, &f)

		if data, err := json.Marshal(&f); err == nil {
			batch.Put([]byte(s), data)
		}

		if i%10000 == 0 {

			if batch.Len() > 0 {
				server.ldb.Write(batch, nil)
				//				batch = new(leveldb.Batch)
				batch.Reset()
			}
			fmt.Println(i, time.Since(t).Seconds())

		}

		//fmt.Println(server.GetFileInfoByMd5(s))

	}

	this.util.WriteFile("time.txt", time.Since(t).String())
	fmt.Println(time.Since(t).String())
}

func (this *Server) RepairStatWeb(w http.ResponseWriter, r *http.Request) {

	this.RepairStat()

	w.Write([]byte("ok"))

}


func (this *Server) Stat(w http.ResponseWriter, r *http.Request) {
	data:=this.util.JsonEncodePretty(this.GetStat())
	w.Write([]byte(data))
}

func (this *Server) GetStat() []StatDateFileInfo {
	var (
		min  int64
		max  int64
		err  error
		i    int64

		rows []StatDateFileInfo
	)
	min = 20190101
	max = 20190101
	for k, _ := range this.statMap.Get() {
		ks := strings.Split(k, "_")
		if len(ks) == 2 {
			if i, err = strconv.ParseInt(ks[0], 10, 64); err != nil {
				continue
			}
			if i >= max {
				max = i
			}
			if i < min {
				min = i
			}

		}

	}

	for i := min; i <= max; i++ {

		s := fmt.Sprintf("%d", i)
		if v, ok := this.statMap.GetValue(s + "_" + CONST_STAT_FILE_TOTAL_SIZE_KEY); ok {
			var info StatDateFileInfo
			info.Date = s
			switch v.(type) {
			case int64:
				info.TotalSize = v.(int64)
			}

			if v, ok := this.statMap.GetValue(s + "_" + CONST_STAT_FILE_COUNT_KEY); ok {
				switch v.(type) {
				case int64:
					info.FileCount = v.(int64)
				}
			}

			rows = append(rows, info)

		}

	}

	if v, ok := this.statMap.GetValue(CONST_STAT_FILE_COUNT_KEY); ok {
		var info StatDateFileInfo
		info.Date = "all"
		info.FileCount = v.(int64)
		if v, ok := this.statMap.GetValue(CONST_STAT_FILE_TOTAL_SIZE_KEY); ok {
			info.TotalSize = v.(int64)
		}
		rows = append(rows, info)
	}

	return rows


}

func (this *Server) RegisterExit() {
	c := make(chan os.Signal)
	signal.Notify(c, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		for s := range c {
			switch s {
			case syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT:
				this.ldb.Close()
				log.Info("Exit", s)
				os.Exit(1)
			}
		}
	}()
}

func (this *Server) Consumer() {

	ConsumerFunc := func() {

		for {
			fileInfo := <-this.queueToPeers
			this.postFileToPeer(&fileInfo)
		}

	}

	for i := 0; i < 50; i++ {

		go ConsumerFunc()

	}

}

func (this *Server) AutoRepair() {

	AutoRepairFunc := func() {

		var (
			dateStats []StatDateFileInfo
			err error
			countKey string
			md5s string
			localSet mapset.Set
			remoteSet mapset.Set
			allSet mapset.Set

		)

		defer func() {
			if re := recover(); re != nil {
				buffer := debug.Stack()
				log.Error("AutoRepair")
				log.Error(re)
				log.Error(string(buffer))
			}
		}()

		Update:= func(peer string, dateStat StatDateFileInfo) {

			req:=httplib.Get(fmt.Sprintf("%s/sync?date=%s&force=%s",peer, dateStat.Date,"1"))
			req.SetTimeout(time.Second*5,time.Second*5)
			if _,err=req.String();err!=nil {
				log.Error(err)

			}
			log.Info(fmt.Sprintf("syn file from %s date %s",peer,dateStat.Date))



		}


		for _,peer:=range Config().Peers {



			req:=httplib.Get(fmt.Sprintf("%s/%s",peer,"stat"))
			req.SetTimeout(time.Second*5,time.Second*5)
			if err=req.ToJSON(&dateStats);err!=nil {
				log.Error(err)
				continue
			}


			for _,dateStat:=range dateStats {
				if dateStat.Date=="all" {
					continue
				}
				countKey=dateStat.Date+"_"+ CONST_STAT_FILE_COUNT_KEY
				if v,ok:= this.statMap.GetValue(countKey);ok {
					switch v.(type) {
					case int64:
						if v.(int64)!=dateStat.FileCount {//不相等,找差异
						//TODO
                            req:= httplib.Post(fmt.Sprintf("%s/get_md5s_by_date",peer))

                            req.Param("date",dateStat.Date)

                            if md5s,err=req.String();err!=nil {
                            	continue
							}
                            if localSet,err= this.GetMd5sByDate(dateStat.Date);err!=nil {
                            	log.Error(err)
                            	continue
							}
                            remoteSet=this.util.StrToMapSet(md5s,",")
                            allSet=localSet.Union(remoteSet)
                           md5s= this.util.MapSetToStr(allSet.Difference(localSet),",")
                           req=httplib.Post(fmt.Sprintf("%s/receive_md5s",peer))
                           req.SetTimeout(time.Second*5,time.Second*5)
                           req.Param("md5s",md5s)
                           req.String()



							//Update(peer,dateStat)
						}
					}
				} else {
					Update(peer,dateStat)

				}

			}

		}

	}

    //for {
    //	time.Sleep(time.Second*10)
	//	AutoRepairFunc()
    //	time.Sleep(time.Minute*60)
	//}


	AutoRepairFunc()
}







func (this *Server) Check() {

	check := func() {

		defer func() {
			if re := recover(); re != nil {
				buffer := debug.Stack()
				log.Error("Check")
				log.Error(re)
				log.Error(string(buffer))
			}
		}()

		var (
			status  Status
			err     error
			subject string
			body    string
			req     *httplib.BeegoHTTPRequest
		)

		for _, peer := range Config().Peers {

			req = httplib.Get(peer + "/status")
			req.SetTimeout(time.Second*5, time.Second*5)
			err = req.ToJSON(&status)

			if status.Status != "ok" {

				for _, to := range Config().AlramReceivers {
					subject = "fastdfs server error"

					if err != nil {
						body = fmt.Sprintf("%s\nserver:%s\nerror:\n%s", subject, peer, err.Error())
					} else {
						body = fmt.Sprintf("%s\nserver:%s\n", subject, peer)
					}
					if err = this.SendToMail(to, subject, body, "text"); err != nil {
						log.Error(err)
					}
				}

				if Config().AlarmUrl != "" {
					req = httplib.Post(Config().AlarmUrl)
					req.SetTimeout(time.Second*10, time.Second*10)
					req.Param("message", body)
					req.Param("subject", subject)
					if _, err = req.String(); err != nil {
						log.Error(err)
					}

				}

			}
		}

	}

	go func() {
		for {
			time.Sleep(time.Minute * 10)
			check()
		}
	}()

}

//func (this *Server) Repair(w http.ResponseWriter, r *http.Request) {
//
//	if this.IsPeer(r) {
//		this.AutoRepair()
//	} else {
//		w.Write([]byte("not permit"))
//	}
//
//}

func (this *Server) Status(w http.ResponseWriter, r *http.Request) {

	var (
		status Status
		err    error
		data   []byte
	)

	status.Status = "ok"

	if data, err = json.Marshal(&status); err != nil {
		status.Status = "fail"
		status.Message = err.Error()
		w.Write(data)
		return
	}
	w.Write(data)

}

func (this *Server) HeartBeat(w http.ResponseWriter, r *http.Request) {

}

func (this *Server) Index(w http.ResponseWriter, r *http.Request) {
	if Config().EnableWebUpload {
		fmt.Fprintf(w,
			fmt.Sprintf(`<html>
	    <head>
	        <meta charset="utf-8"></meta>
	        <title>Uploader</title>
			<style>
			form {
				bargin
				
			}
			 .form-line {
				display:block;
			}
			</style>
	    </head>
	    <body>
	        <form action="/upload" method="post" enctype="multipart/form-data">
	            <span class="form-line">文件(file):<input  type="file" id="file" name="file" ></span>
				<span class="form-line">场景(scene):<input  type="text" id="scene" name="scene" value="%s"></span>
				<span class="form-line">输出(output):<input  type="text" id="output" name="output" value="json"></span>
				<span class="form-line">自定义路径(path):<input  type="text" id="path" name="path" value=""></span>
	            <input type="submit" name="submit" value="upload">
	        </form>
	    </body>
	</html>`, Config().DefaultScene))
	} else {
		w.Write([]byte("web upload deny"))
	}
}

func init() {

	for _, folder := range FOLDERS {
		os.Mkdir(folder, 0666)
	}
	flag.Parse()

	if !server.util.FileExists(CONST_CONF_FILE_NAME) {

		peer := "http://" + server.util.GetPulicIP() + ":8080"

		cfg := fmt.Sprintf(cfgJson, peer)

		server.util.WriteFile(CONST_CONF_FILE_NAME, cfg)
	}

	if logger, err := log.LoggerFromConfigAsBytes([]byte(logConfigStr)); err != nil {
		panic(err)

	} else {
		log.ReplaceLogger(logger)
	}

	if _logacc, err := log.LoggerFromConfigAsBytes([]byte(logAccessConfigStr)); err == nil {
		logacc = _logacc
		log.Info("succes init log access")

	} else {
		log.Error(err.Error())
	}

	ParseConfig(CONST_CONF_FILE_NAME)

	if Config().QueueSize == 0 {
		Config().QueueSize = CONST_QUEUE_SIZE
	}

	staticHandler = http.StripPrefix("/"+Config().Group+"/", http.FileServer(http.Dir(STORE_DIR)))

	server.initComponent(false)
}

func (this *Server)initComponent(is_reload bool) {
	var (
		err   error
		ldb   *leveldb.DB
		ip    string
		stat  map[string]interface{}
		data  []byte
		count int64
	)
	ip = this.util.GetPulicIP()
	ex, _ := regexp.Compile("\\d+\\.\\d+\\.\\d+\\.\\d+")
	var peers []string
	for _, peer := range Config().Peers {
		if this.util.Contains(ip, ex.FindAllString(peer, -1)) {
			continue
		}
		if strings.HasPrefix(peer, "http") {
			peers = append(peers, peer)
		} else {
			peers = append(peers, "http://"+peer)
		}
	}
	Config().Peers = peers
	if !is_reload {
		ldb, err = leveldb.OpenFile(CONST_LEVELDB_FILE_NAME, nil)
		if err != nil {
			log.Error(err)
			panic(err)
		}
		server.ldb = ldb
	}

	FormatStatInfo := func() {

		if this.util.FileExists(CONST_STAT_FILE_NAME) {
			if data, err = this.util.ReadBinFile(CONST_STAT_FILE_NAME); err != nil {
				log.Error(err)
			} else {

				if err = json.Unmarshal(data, &stat); err != nil {
					log.Error(err)
				} else {
					for k, v := range stat {
						switch v.(type) {
						case float64:
							vv := strings.Split(fmt.Sprintf("%f", v), ".")[0]

							if count, err = strconv.ParseInt(vv, 10, 64); err != nil {
								log.Error(err)
							} else {
								this.statMap.Put(k, count)
							}

						default:
							this.statMap.Put(k, v)

						}

					}
				}
			}

		} else {
			this.RepairStat()
		}

	}
	if !is_reload {
		FormatStatInfo()
	}
	//Timer

}

type HttpHandler struct {
}

func (HttpHandler) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	status_code := "200"
	defer func(t time.Time) {
		logStr := fmt.Sprintf("[Access] %s | %v | %s | %s | %s | %s |%s",
			time.Now().Format("2006/01/02 - 15:04:05"),
			res.Header(),
			time.Since(t).String(),
			server.util.GetClientIp(req),
			req.Method,
			status_code,
			req.RequestURI,
		)

		logacc.Info(logStr)
	}(time.Now())

	defer func() {
		if err := recover(); err != nil {
			status_code = "500"
			res.WriteHeader(500)
			print(err)
			buff := debug.Stack()
			log.Error(err)
			log.Error(string(buff))

		}
	}()

	http.DefaultServeMux.ServeHTTP(res, req)
}

func (this *Server) Main() {


	go func() {
		for {
			this.CheckFileAndSendToPeer("", false)
			time.Sleep(time.Second * time.Duration(Config().RefreshInterval))
			this.util.RemoveEmptyDir(STORE_DIR)
		}
	}()

	go this.Check()
	go this.Consumer()
	//if Config().AutoRepair {
	//	go func() {
	//		for {
	//			time.Sleep(time.Second*10)
	//			this.AutoRepair()
	//			time.Sleep(time.Minute*60)
	//		}
	//	}()
	//
	//}

	http.HandleFunc("/", this.Index)
	http.HandleFunc("/check_file_exist", this.CheckFileExist)
	http.HandleFunc("/upload", this.Upload)
	http.HandleFunc("/delete", this.RemoveFile)
	http.HandleFunc("/sync", this.Sync)
	http.HandleFunc("/stat", this.Stat)
	http.HandleFunc("/repair_stat", this.RepairStatWeb)
	http.HandleFunc("/status", this.Status)
	//http.HandleFunc("/repair", this.Repair)
	http.HandleFunc("/syncfile", this.SyncFile)
	http.HandleFunc("/get_md5s_by_date", this.GetMd5sForWeb)
	http.HandleFunc("/receive_md5s", this.ReceiveMd5s)
	http.HandleFunc("/"+Config().Group+"/", this.Download)
	fmt.Println("Listen on " + Config().Addr)
	err := http.ListenAndServe(Config().Addr, new(HttpHandler))
	log.Error(err)
	fmt.Println(err)
}

func main() {
	server.Main()

}
