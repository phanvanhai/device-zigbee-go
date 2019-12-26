package packet

import (
	"strconv"
	"sync"
	"time"
)

const (
	prefixRepoNameWithID  = "_id_"
	prefixRepoNameWithMAC = "_mac_"
	prefixRepoNameWithCMD = "_cmd_"
)

var once sync.Once
var rp *repoStruct

type ContentRepoStruct struct {
	Packet interface{} `json:"content"`
	Cmd    int8        `json:"cmd"`
}

type repoStruct struct {
	repo sync.Map
}

type RepoInterface interface {
	SendToRepo(nameRepo string, content interface{})
	GetFromRepo(nameRepo string) (interface{}, bool)
	GetFromRepoAfterResetWithTime(nameRepo string, countRetry int32, timeMsStep int32) (interface{}, bool)
	ResetRepo(nameRepo string)
	GetRepoNameByID(id string) string
	GetRepoNameByMAC(mac int64) string
	GetRepoNameByCMD(cmd int8) string
}

func Repo() RepoInterface {
	if rp == nil {
		once.Do(func() {
			rp = new(repoStruct)
		})
	}
	return rp
}

func (r *repoStruct) SendToRepo(nameRepo string, content interface{}) {
	r.repo.LoadOrStore(nameRepo, content)
}

func (r *repoStruct) GetFromRepo(nameRepo string) (interface{}, bool) {
	c, ok := r.repo.Load(nameRepo)
	if c == nil {
		ok = false
	}
	return c, ok
}

func (r *repoStruct) GetFromRepoAfterResetWithTime(nameRepo string, countRetry int32, timeMsStep int32) (result interface{}, ok bool) {
	ok = false
	if countRetry < 0 || timeMsStep < 0 {
		return nil, false
	}

	r.ResetRepo(nameRepo)
	for ; countRetry >= 0; countRetry-- {
		result, ok = r.GetFromRepo(nameRepo)
		if ok {
			break
		}
		time.Sleep(time.Duration(timeMsStep) * time.Millisecond)
	}
	return
}

func (r *repoStruct) ResetRepo(nameRepo string) {
	r.repo.Delete(nameRepo)
}

func (r *repoStruct) GetRepoNameByID(id string) string {
	return prefixRepoNameWithID + id
}

func (r *repoStruct) GetRepoNameByMAC(mac int64) string {
	return prefixRepoNameWithMAC + strconv.FormatInt(mac, 16)
}

func (r *repoStruct) GetRepoNameByCMD(cmd int8) string {
	return prefixRepoNameWithCMD + strconv.FormatInt(int64(cmd), 10)
}
