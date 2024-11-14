package mgr

import (
	"strings"

	"github.com/ludoux/ngapost2md/config"
	"github.com/ludoux/ngapost2md/nga"
	"gopkg.in/ini.v1"
)

type ClientExt struct {
	*nga.NgaClient
	Cfg *ini.File
}

func InitNGA() (*ClientExt, error) {
	cfg, err := config.GetConfigAutoUpdate()
	if err != nil {
		if strings.Contains(err.Error(), "no such file") {
			err = config.SaveDefaultConfigFile()
			if err != nil {
				return nil, err
			} else {
				cfg, err = config.GetConfigAutoUpdate()
				if err != nil {
					return nil, err
				}
			}
		} else {
			return nil, err
		}
	}

	var netword = cfg.Section("network")
	//Cookie
	var ngaPassportUid = netword.Key("ngaPassportUid").String()
	var ngaPassportCid = netword.Key("ngaPassportCid").String()
	nga.COOKIE = "ngaPassportUid=" + ngaPassportUid + ";ngaPassportCid=" + ngaPassportCid

	nga.BASE_URL = netword.Key("base_url").String()
	nga.UA = netword.Key("ua").String()
	//默认线程数为2,仅支持1~3
	nga.CFGFILE_THREAD_COUNT = netword.Key("thread").InInt(1, []int{1, 2, 3})
	nga.CFGFILE_PAGE_DOWNLOAD_LIMIT = netword.Key("page_download_limit").RangeInt(100, -1, 100)
	var post = cfg.Section("post")
	nga.CFGFILE_GET_IP_LOCATION = post.Key("get_ip_location").MustBool()
	nga.CFGFILE_ENHANCE_ORI_REPLY = post.Key("enhance_ori_reply").MustBool()
	nga.CFGFILE_USE_LOCAL_SMILE_PIC = post.Key("use_local_smile_pic").MustBool()
	nga.CFGFILE_LOCAL_SMILE_PIC_PATH = post.Key("local_smile_pic_path").String()
	nga.CFGFILE_USE_TITLE_AS_FOLDER_NAME = post.Key("use_title_as_folder_name").MustBool()
	nga.CFGFILE_USE_TITLE_AS_MD_FILE_NAME = post.Key("use_title_as_md_file_name").MustBool()
	nga.Client = nga.NewNgaClient()

	return &ClientExt{
		NgaClient: nga.Client,
		Cfg:       cfg,
	}, nil
}
