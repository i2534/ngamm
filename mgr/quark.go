package mgr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"os"
	"regexp"
	"strings"
	"time"
)

// Copy from: https://github.com/Cp0204/quark-auto-save/blob/main/quark_auto_save.py ffe95fc class Quark:
// Transfer by AI
// Quark 类的Go实现
type Quark struct {
	BaseURL     string
	BaseURLApp  string
	UserAgent   string
	Cookie      string
	Index       int
	IsActive    bool
	Nickname    string
	Mparam      map[string]string
	SavepathFid map[string]string
	client      *http.Client
}

// 初始化Quark实例
func NewQuark(cookie string) *Quark {
	jar, _ := cookiejar.New(nil)
	q := &Quark{
		BaseURL:     "https://drive-pc.quark.cn",
		BaseURLApp:  "https://drive-m.quark.cn",
		UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) quark-cloud-drive/3.14.2 Chrome/112.0.5615.165 Electron/24.1.3.8 Safari/537.36 Channel/pckk_other_ch",
		Cookie:      strings.TrimSpace(cookie),
		Index:       1,
		IsActive:    false,
		Nickname:    "",
		Mparam:      make(map[string]string),
		SavepathFid: map[string]string{"/": "0"},
		client:      &http.Client{Jar: jar, Timeout: 10 * time.Second},
	}
	q.matchMparamFromCookie(cookie)
	return q
}

// 从cookie中提取移动端参数
func (q *Quark) matchMparamFromCookie(cookie string) {
	kpsRegex := regexp.MustCompile(`(^|[^a-zA-Z0-9])kps=([a-zA-Z0-9%+/=]+)[;&]?`)
	signRegex := regexp.MustCompile(`(^|[^a-zA-Z0-9])sign=([a-zA-Z0-9%+/=]+)[;&]?`)
	vcodeRegex := regexp.MustCompile(`(^|[^a-zA-Z0-9])vcode=([a-zA-Z0-9%+/=]+)[;&]?`)

	if kpsMatch := kpsRegex.FindStringSubmatch(cookie); len(kpsMatch) > 1 {
		if signMatch := signRegex.FindStringSubmatch(cookie); len(signMatch) > 1 {
			if vcodeMatch := vcodeRegex.FindStringSubmatch(cookie); len(vcodeMatch) > 1 {
				q.Mparam = map[string]string{
					"kps":   strings.ReplaceAll(kpsMatch[1], "%25", "%"),
					"sign":  strings.ReplaceAll(signMatch[1], "%25", "%"),
					"vcode": strings.ReplaceAll(vcodeMatch[1], "%25", "%"),
				}
			}
		}
	}
}

// 发送HTTP请求
func (q *Quark) sendRequest(method, urlStr string, params map[string]string, jsonData any, headers map[string]string) (*http.Response, error) {
	var reqBody io.Reader

	// 处理JSON数据
	if jsonData != nil {
		jsonBytes, err := json.Marshal(jsonData)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewBuffer(jsonBytes)
	}

	// 创建请求
	req, err := http.NewRequest(method, urlStr, reqBody)
	if err != nil {
		return nil, err
	}

	// 设置默认头
	req.Header.Set("cookie", q.Cookie)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("user-agent", q.UserAgent)

	// 应用自定义头
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// 添加查询参数
	if params != nil {
		query := req.URL.Query()
		for k, v := range params {
			query.Add(k, v)
		}
		req.URL.RawQuery = query.Encode()
	}

	// 对特定URL进行处理
	if len(q.Mparam) > 0 && strings.Contains(urlStr, "share") && strings.Contains(urlStr, q.BaseURL) {
		urlStr = strings.ReplaceAll(urlStr, q.BaseURL, q.BaseURLApp)

		// 创建新请求
		req, err = http.NewRequest(method, urlStr, reqBody)
		if err != nil {
			return nil, err
		}

		// 重新设置头部
		req.Header.Set("content-type", "application/json")
		req.Header.Set("user-agent", q.UserAgent)
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		// 添加特定的参数
		query := req.URL.Query()
		for k, v := range params {
			query.Add(k, v)
		}

		// 添加移动特有参数
		query.Add("device_model", "M2011K2C")
		query.Add("entry", "default_clouddrive")
		query.Add("_t_group", "0%3A_s_vp%3A1")
		query.Add("dmn", "Mi%2B11")
		query.Add("fr", "android")
		query.Add("pf", "3300")
		query.Add("bi", "35937")
		query.Add("ve", "7.4.5.680")
		query.Add("ss", "411x875")
		query.Add("mi", "M2011K2C")
		query.Add("nt", "5")
		query.Add("nw", "0")
		query.Add("kt", "4")
		query.Add("pr", "ucpro")
		query.Add("sv", "release")
		query.Add("dt", "phone")
		query.Add("data_from", "ucapi")
		query.Add("kps", q.Mparam["kps"])
		query.Add("sign", q.Mparam["sign"])
		query.Add("vcode", q.Mparam["vcode"])
		query.Add("app", "clouddrive")
		query.Add("kkkk", "1")

		req.URL.RawQuery = query.Encode()
		req.Header.Del("cookie") // 移除cookie头
	}

	// 发送请求
	resp, err := q.client.Do(req)
	if err != nil {
		fmt.Printf("Send request error: %v\n", err)
		return nil, err
	}

	return resp, nil
}

// 初始化账号
func (q *Quark) Init() (map[string]any, error) {
	info, err := q.GetAccountInfo()
	if err != nil {
		return nil, err
	}
	if info != nil {
		q.IsActive = true
		q.Nickname = info["nickname"].(string)
		return info, nil
	}
	return nil, nil
}

// 获取账号信息
func (q *Quark) GetAccountInfo() (map[string]any, error) {
	urlStr := "https://pan.quark.cn/account/info"
	params := map[string]string{
		"fr":       "pc",
		"platform": "pc",
	}

	resp, err := q.sendRequest("GET", urlStr, params, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	success := result["success"].(bool)
	if !success {
		code := result["code"].(string)
		msg := result["msg"].(string)
		fmt.Printf("Quark: 获取账号信息失败, code: %s, msg: %s\n", code, msg)
		return nil, fmt.Errorf("获取账号信息失败, code: %s, msg: %s", code, msg)
	}

	if data, ok := result["data"].(map[string]any); ok {
		return data, nil
	}
	return nil, nil
}

// 获取成长信息
func (q *Quark) GetGrowthInfo() map[string]any {
	urlStr := fmt.Sprintf("%s/1/clouddrive/capacity/growth/info", q.BaseURLApp)
	params := map[string]string{
		"pr":    "ucpro",
		"fr":    "android",
		"kps":   q.Mparam["kps"],
		"sign":  q.Mparam["sign"],
		"vcode": q.Mparam["vcode"],
	}

	headers := map[string]string{
		"content-type": "application/json",
	}

	resp, err := q.sendRequest("GET", urlStr, params, nil, headers)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil
	}

	if data, ok := result["data"].(map[string]any); ok {
		return data
	}
	return nil
}

// 每日签到
func (q *Quark) GetGrowthSign() (bool, any) {
	urlStr := fmt.Sprintf("%s/1/clouddrive/capacity/growth/sign", q.BaseURLApp)
	params := map[string]string{
		"pr":    "ucpro",
		"fr":    "android",
		"kps":   q.Mparam["kps"],
		"sign":  q.Mparam["sign"],
		"vcode": q.Mparam["vcode"],
	}

	payload := map[string]any{
		"sign_cyclic": true,
	}

	headers := map[string]string{
		"content-type": "application/json",
	}

	resp, err := q.sendRequest("POST", urlStr, params, payload, headers)
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err.Error()
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return false, err.Error()
	}

	if data, ok := result["data"].(map[string]any); ok {
		return true, data["sign_daily_reward"]
	}
	return false, result["message"]
}

// 获取分享token
func (q *Quark) GetStoken(pwdID, passcode string) (bool, string) {
	urlStr := fmt.Sprintf("%s/1/clouddrive/share/sharepage/token", q.BaseURL)
	params := map[string]string{
		"pr": "ucpro",
		"fr": "pc",
	}

	payload := map[string]string{
		"pwd_id":   pwdID,
		"passcode": passcode,
	}

	resp, err := q.sendRequest("POST", urlStr, params, payload, nil)
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err.Error()
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return false, err.Error()
	}

	if result["status"].(float64) == 200 {
		data := result["data"].(map[string]any)
		return true, data["stoken"].(string)
	}
	return false, result["message"].(string)
}

// 获取分享详情
func (q *Quark) GetDetail(pwdID, stoken, pdirFid string, fetchShare int) map[string]any {
	urlStr := fmt.Sprintf("%s/1/clouddrive/share/sharepage/detail", q.BaseURL)
	listMerge := []any{}
	page := 1

	for {
		params := map[string]string{
			"pr":            "ucpro",
			"fr":            "pc",
			"pwd_id":        pwdID,
			"stoken":        stoken,
			"pdir_fid":      pdirFid,
			"force":         "0",
			"_page":         fmt.Sprintf("%d", page),
			"_size":         "50",
			"_fetch_banner": "0",
			"_fetch_share":  fmt.Sprintf("%d", fetchShare),
			"_fetch_total":  "1",
			"_sort":         "file_type:asc,updated_at:desc",
		}

		resp, err := q.sendRequest("GET", urlStr, params, nil, nil)
		if err != nil {
			break
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			break
		}

		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			break
		}

		data := result["data"].(map[string]any)
		fileList := data["list"].([]any)

		if len(fileList) > 0 {
			listMerge = append(listMerge, fileList...)
			page++
		} else {
			break
		}

		metadata := result["metadata"].(map[string]any)
		if len(listMerge) >= int(metadata["_total"].(float64)) {
			break
		}
	}

	result := make(map[string]any)
	result["list"] = listMerge
	return result
}

// 获取文件ID列表
func (q *Quark) GetFids(filePaths []string) []map[string]any {
	var fids []map[string]any

	for len(filePaths) > 0 {
		var batch []string
		if len(filePaths) > 50 {
			batch = filePaths[:50]
			filePaths = filePaths[50:]
		} else {
			batch = filePaths
			filePaths = []string{}
		}

		urlStr := fmt.Sprintf("%s/1/clouddrive/file/info/path_list", q.BaseURL)
		params := map[string]string{
			"pr": "ucpro",
			"fr": "pc",
		}

		payload := map[string]any{
			"file_path": batch,
			"namespace": "0",
		}

		resp, err := q.sendRequest("POST", urlStr, params, payload, nil)
		if err != nil {
			fmt.Printf("获取目录ID：失败, %v\n", err)
			break
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("获取目录ID：失败, %v\n", err)
			break
		}

		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			fmt.Printf("获取目录ID：失败, %v\n", err)
			break
		}

		if result["code"].(float64) == 0 {
			data := result["data"].([]any)
			for _, item := range data {
				fids = append(fids, item.(map[string]any))
			}
		} else {
			fmt.Printf("获取目录ID：失败, %s\n", result["message"])
			break
		}
	}

	return fids
}

// 列出目录内容
func (q *Quark) LsDir(pdirFid string, fetchFullPath int) []map[string]any {
	var fileList []map[string]any
	if pdirFid == "" {
		return fileList
	}

	page := 1
	for {
		urlStr := fmt.Sprintf("%s/1/clouddrive/file/sort", q.BaseURL)
		params := map[string]string{
			"pr":               "ucpro",
			"fr":               "pc",
			"uc_param_str":     "",
			"pdir_fid":         pdirFid,
			"_page":            fmt.Sprintf("%d", page),
			"_size":            "50",
			"_fetch_total":     "1",
			"_fetch_sub_dirs":  "0",
			"_sort":            "file_type:asc,updated_at:desc",
			"_fetch_full_path": fmt.Sprintf("%d", fetchFullPath),
		}

		resp, err := q.sendRequest("GET", urlStr, params, nil, nil)
		if err != nil {
			break
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			break
		}

		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			break
		}

		data := result["data"].(map[string]any)
		files := data["list"].([]any)

		if len(files) > 0 {
			for _, file := range files {
				fileList = append(fileList, file.(map[string]any))
			}
			page++
		} else {
			break
		}

		metadata := result["metadata"].(map[string]any)
		if len(fileList) >= int(metadata["_total"].(float64)) {
			break
		}
	}

	return fileList
}

// 保存分享文件
func (q *Quark) SaveFile(fidList []string, fidTokenList []string, toPdirFid, pwdID, stoken string) map[string]any {
	urlStr := fmt.Sprintf("%s/1/clouddrive/share/sharepage/save", q.BaseURL)
	params := map[string]string{
		"pr":           "ucpro",
		"fr":           "pc",
		"uc_param_str": "",
		"app":          "clouddrive",
		"__dt":         fmt.Sprintf("%d", int(rand.Float64()*4+1)*60*1000),
		"__t":          fmt.Sprintf("%.0f", float64(time.Now().UnixNano())/1e6),
	}

	payload := map[string]any{
		"fid_list":       fidList,
		"fid_token_list": fidTokenList,
		"to_pdir_fid":    toPdirFid,
		"pwd_id":         pwdID,
		"stoken":         stoken,
		"pdir_fid":       "0",
		"scene":          "link",
	}

	resp, err := q.sendRequest("POST", urlStr, params, payload, nil)
	if err != nil {
		return map[string]any{"code": 500, "message": err.Error()}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return map[string]any{"code": 500, "message": err.Error()}
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return map[string]any{"code": 500, "message": err.Error()}
	}

	return result
}

// 查询任务
func (q *Quark) QueryTask(taskID string) map[string]any {
	retryIndex := 0

	for {
		urlStr := fmt.Sprintf("%s/1/clouddrive/task", q.BaseURL)
		params := map[string]string{
			"pr":           "ucpro",
			"fr":           "pc",
			"uc_param_str": "",
			"task_id":      taskID,
			"retry_index":  fmt.Sprintf("%d", retryIndex),
			"__dt":         fmt.Sprintf("%d", int(rand.Float64()*4+1)*60*1000),
			"__t":          fmt.Sprintf("%.0f", float64(time.Now().UnixNano())/1e6),
		}

		resp, err := q.sendRequest("GET", urlStr, params, nil, nil)
		if err != nil {
			return map[string]any{"code": 500, "message": err.Error()}
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return map[string]any{"code": 500, "message": err.Error()}
		}

		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			return map[string]any{"code": 500, "message": err.Error()}
		}

		data := result["data"].(map[string]any)
		if data["status"].(float64) != 0 {
			if retryIndex > 0 {
				fmt.Println()
			}
			return result
		} else {
			if retryIndex == 0 {
				fmt.Printf("正在等待[%s]执行结果", data["task_title"])
			} else {
				fmt.Print(".")
			}
			retryIndex++
			time.Sleep(500 * time.Millisecond)
		}
	}
}

// 下载文件
func (q *Quark) Download(fids []string) (map[string]any, string) {
	urlStr := fmt.Sprintf("%s/1/clouddrive/file/download", q.BaseURL)
	params := map[string]string{
		"pr":           "ucpro",
		"fr":           "pc",
		"uc_param_str": "",
	}

	payload := map[string]any{
		"fids": fids,
	}

	resp, err := q.sendRequest("POST", urlStr, params, payload, nil)
	if err != nil {
		return map[string]any{"code": 500, "message": err.Error()}, ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return map[string]any{"code": 500, "message": err.Error()}, ""
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return map[string]any{"code": 500, "message": err.Error()}, ""
	}

	// 获取cookies
	setCookies := resp.Cookies()
	var cookieStrParts []string
	for _, cookie := range setCookies {
		cookieStrParts = append(cookieStrParts, fmt.Sprintf("%s=%s", cookie.Name, cookie.Value))
	}
	cookieStr := strings.Join(cookieStrParts, "; ")

	return result, cookieStr
}

// 创建目录
func (q *Quark) Mkdir(dirPath string) map[string]any {
	urlStr := fmt.Sprintf("%s/1/clouddrive/file", q.BaseURL)
	params := map[string]string{
		"pr":           "ucpro",
		"fr":           "pc",
		"uc_param_str": "",
	}

	payload := map[string]any{
		"pdir_fid":      "0",
		"file_name":     "",
		"dir_path":      dirPath,
		"dir_init_lock": false,
	}

	resp, err := q.sendRequest("POST", urlStr, params, payload, nil)
	if err != nil {
		return map[string]any{"code": 500, "message": err.Error()}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return map[string]any{"code": 500, "message": err.Error()}
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return map[string]any{"code": 500, "message": err.Error()}
	}

	return result
}

// 重命名文件
func (q *Quark) Rename(fid, fileName string) map[string]any {
	urlStr := fmt.Sprintf("%s/1/clouddrive/file/rename", q.BaseURL)
	params := map[string]string{
		"pr":           "ucpro",
		"fr":           "pc",
		"uc_param_str": "",
	}

	payload := map[string]any{
		"fid":       fid,
		"file_name": fileName,
	}

	resp, err := q.sendRequest("POST", urlStr, params, payload, nil)
	if err != nil {
		return map[string]any{"code": 500, "message": err.Error()}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return map[string]any{"code": 500, "message": err.Error()}
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return map[string]any{"code": 500, "message": err.Error()}
	}

	return result
}

// 删除文件
func (q *Quark) Delete(fileList []string) map[string]any {
	urlStr := fmt.Sprintf("%s/1/clouddrive/file/delete", q.BaseURL)
	params := map[string]string{
		"pr":           "ucpro",
		"fr":           "pc",
		"uc_param_str": "",
	}

	payload := map[string]any{
		"action_type":  2,
		"filelist":     fileList,
		"exclude_fids": []string{},
	}

	resp, err := q.sendRequest("POST", urlStr, params, payload, nil)
	if err != nil {
		return map[string]any{"code": 500, "message": err.Error()}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return map[string]any{"code": 500, "message": err.Error()}
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return map[string]any{"code": 500, "message": err.Error()}
	}

	return result
}

// 回收站列表
func (q *Quark) RecycleList(page, size int) []map[string]any {
	urlStr := fmt.Sprintf("%s/1/clouddrive/file/recycle/list", q.BaseURL)
	params := map[string]string{
		"_page":        fmt.Sprintf("%d", page),
		"_size":        fmt.Sprintf("%d", size),
		"pr":           "ucpro",
		"fr":           "pc",
		"uc_param_str": "",
	}

	resp, err := q.sendRequest("GET", urlStr, params, nil, nil)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil
	}

	data := result["data"].(map[string]any)
	fileList := data["list"].([]any)

	var resultList []map[string]any
	for _, file := range fileList {
		resultList = append(resultList, file.(map[string]any))
	}

	return resultList
}

// 清空回收站
func (q *Quark) RecycleRemove(recordList []string) map[string]any {
	urlStr := fmt.Sprintf("%s/1/clouddrive/file/recycle/remove", q.BaseURL)
	params := map[string]string{
		"uc_param_str": "",
		"fr":           "pc",
		"pr":           "ucpro",
	}

	payload := map[string]any{
		"select_mode": 2,
		"record_list": recordList,
	}

	resp, err := q.sendRequest("POST", urlStr, params, payload, nil)
	if err != nil {
		return map[string]any{"code": 500, "message": err.Error()}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return map[string]any{"code": 500, "message": err.Error()}
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return map[string]any{"code": 500, "message": err.Error()}
	}

	return result
}

// 从URL获取分享ID
func (q *Quark) GetIDFromURL(shareURL string) (string, string, string) {
	shareURL = strings.Replace(shareURL, "https://pan.quark.cn/s/", "", 1)
	pattern := `(\w+)(\?pwd=(\w+))?(#/list/share.*/(\w+))?`

	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(shareURL)

	if len(matches) > 0 {
		pwdID := matches[1]
		passcode := ""
		pdirFid := "0"

		if len(matches) > 3 && matches[3] != "" {
			passcode = matches[3]
		}

		if len(matches) > 5 && matches[5] != "" {
			pdirFid = matches[5]
		}

		return pwdID, passcode, pdirFid
	}

	return "", "", ""
}

// 更新保存路径FID字典
func (q *Quark) UpdateSavepathFid(tasklist []map[string]any) bool {
	var dirPaths []string

	// 获取所有需要的路径
	for _, task := range tasklist {
		savePath := task["savepath"].(string)
		savePath = regexp.MustCompile(`/{2,}`).ReplaceAllString("/"+savePath, "/")

		// 检查任务是否过期
		if endDate, ok := task["enddate"]; ok && endDate.(string) != "" {
			endDateTime, err := time.Parse("2006-01-02", endDate.(string))
			if err == nil && time.Now().After(endDateTime) {
				continue
			}
		}

		dirPaths = append(dirPaths, savePath)
	}

	if len(dirPaths) == 0 {
		return false
	}

	// 获取已存在的目录FID
	dirPathsExistArr := q.GetFids(dirPaths)
	var dirPathsExist []string
	for _, item := range dirPathsExistArr {
		dirPathsExist = append(dirPathsExist, item["file_path"].(string))
	}

	// 找出不存在的目录并创建它们
	for _, dirPath := range dirPaths {
		if dirPath == "/" {
			continue
		}

		exists := false
		for _, existPath := range dirPathsExist {
			if dirPath == existPath {
				exists = true
				break
			}
		}

		if !exists {
			mkdirReturn := q.Mkdir(dirPath)
			if mkdirReturn["code"].(float64) == 0 {
				newDir := mkdirReturn["data"].(map[string]any)
				dirPathsExistArr = append(dirPathsExistArr, map[string]any{
					"file_path": dirPath,
					"fid":       newDir["fid"].(string),
				})
				fmt.Printf("创建文件夹：%s\n", dirPath)
			} else {
				fmt.Printf("创建文件夹：%s 失败, %s\n", dirPath, mkdirReturn["message"].(string))
			}
		}
	}

	// 保存目录的FID
	for _, dirPath := range dirPathsExistArr {
		q.SavepathFid[dirPath["file_path"].(string)] = dirPath["fid"].(string)
	}

	return true
}

// 测试保存功能
func (q *Quark) DoSaveCheck(shareURL, savePath string) map[string]any {
	pwdID, passcode, pdirFid := q.GetIDFromURL(shareURL)
	isSharing, stoken := q.GetStoken(pwdID, passcode)

	if !isSharing {
		return nil
	}

	shareDetail := q.GetDetail(pwdID, stoken, pdirFid, 0)
	shareFileList := shareDetail["list"].([]any)

	if len(shareFileList) == 0 {
		return nil
	}

	var fidList []string
	var fidTokenList []string
	var fileNameList []string

	for _, file := range shareFileList {
		fileMap := file.(map[string]any)
		fidList = append(fidList, fileMap["fid"].(string))
		fidTokenList = append(fidTokenList, fileMap["share_fid_token"].(string))
		fileNameList = append(fileNameList, fileMap["file_name"].(string))
	}

	// 获取目标目录FID
	getFids := q.GetFids([]string{savePath})
	var toPdirFid string

	if len(getFids) > 0 {
		toPdirFid = getFids[0]["fid"].(string)
	} else {
		mkdirResult := q.Mkdir(savePath)
		if mkdirResult["code"].(float64) == 0 {
			toPdirFid = mkdirResult["data"].(map[string]any)["fid"].(string)
		} else {
			return nil
		}
	}

	// 保存文件
	saveFile := q.SaveFile(fidList, fidTokenList, toPdirFid, pwdID, stoken)

	if saveFile["code"].(float64) == 41017 {
		return nil
	} else if saveFile["code"].(float64) == 0 {
		// 获取目标目录下的文件列表
		dirFileList := q.LsDir(toPdirFid, 0)
		var delList []string

		// 找出刚保存的文件
		now := time.Now().Unix()
		for _, file := range dirFileList {
			fileName := file["file_name"].(string)
			createdAt := int64(file["created_at"].(float64))

			// 检查文件名是否在要保存的列表中，且创建时间在60秒内
			for _, saveFileName := range fileNameList {
				if fileName == saveFileName && (now-createdAt < 60) {
					delList = append(delList, file["fid"].(string))
					break
				}
			}
		}

		// 删除文件并清空回收站
		if len(delList) > 0 {
			q.Delete(delList)
			recycleList := q.RecycleList(1, 30)
			var recordIDList []string

			for _, item := range recycleList {
				fid := item["fid"].(string)
				for _, delFid := range delList {
					if fid == delFid {
						recordIDList = append(recordIDList, item["record_id"].(string))
						break
					}
				}
			}

			if len(recordIDList) > 0 {
				q.RecycleRemove(recordIDList)
			}
		}

		return saveFile
	}

	return nil
}

// 格式化字节大小
func FormatBytes(sizeBytes float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"}
	i := 0

	for sizeBytes >= 1024 && i < len(units)-1 {
		sizeBytes /= 1024
		i++
	}

	return fmt.Sprintf("%.2f %s", sizeBytes, units[i])
}

// 执行签到
func DoSign(account *Quark) {
	if len(account.Mparam) == 0 {
		fmt.Println("⏭️ 移动端参数未设置，跳过签到")
		fmt.Println()
		return
	}

	// 获取成长信息
	growthInfo := account.GetGrowthInfo()
	if growthInfo != nil {
		// 判断是否为VIP
		isVip := false
		if vipStatus, ok := growthInfo["88VIP"].(bool); ok {
			isVip = vipStatus
		}

		// 总空间
		totalCapacity := growthInfo["total_capacity"].(float64)

		// 签到获得空间
		var signReward float64
		if capComp, ok := growthInfo["cap_composition"].(map[string]any); ok {
			if reward, ok := capComp["sign_reward"].(float64); ok {
				signReward = reward
			}
		}

		growthMessage := fmt.Sprintf("💾 %s 总空间：%s，签到累计获得：%s",
			map[bool]string{true: "88VIP", false: "普通用户"}[isVip],
			FormatBytes(totalCapacity),
			FormatBytes(signReward))

		// 签到信息
		capSign := growthInfo["cap_sign"].(map[string]any)
		signDaily := capSign["sign_daily"].(bool)
		signDailyReward := capSign["sign_daily_reward"].(float64)
		signProgress := int(capSign["sign_progress"].(float64))
		signTarget := int(capSign["sign_target"].(float64))

		if signDaily {
			// 已经签到
			signMessage := fmt.Sprintf("📅 签到记录: 今日已签到+%dMB，连签进度(%d/%d)✅",
				int(signDailyReward/1024/1024),
				signProgress,
				signTarget)

			message := fmt.Sprintf("%s\n%s", signMessage, growthMessage)
			fmt.Println(message)
		} else {
			// 执行签到
			sign, signReturn := account.GetGrowthSign()
			if sign {
				signRewardMB := int(signReturn.(float64) / 1024 / 1024)
				signMessage := fmt.Sprintf("📅 执行签到: 今日签到+%dMB，连签进度(%d/%d)✅",
					signRewardMB,
					signProgress+1,
					signTarget)

				message := fmt.Sprintf("%s\n%s", signMessage, growthMessage)

				// 检查是否需要推送通知
				signNotify := os.Getenv("QUARK_SIGN_NOTIFY")
				if signNotify == "false" {
					fmt.Println(message)
				} else {
					// 这里应该实现推送通知的功能
					message = strings.Replace(message, "今日", fmt.Sprintf("[%s]今日", account.Nickname), 1)
					fmt.Println(message) // 临时简单输出
				}
			} else {
				fmt.Printf("📅 签到异常: %v\n", signReturn)
			}
		}
	}
	fmt.Println()
}
