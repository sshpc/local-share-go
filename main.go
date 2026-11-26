package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/skip2/go-qrcode"
)

// 配置项（可通过配置文件或环境变量覆盖）
type Config struct {
	ServerAddr    string `json:"server_addr"`     // 服务地址和端口
	UploadDir     string `json:"upload_dir"`      // 文件存储目录
	PublicBaseURL string `json:"public_base_url"` // 公网可访问的地址
}

// 加载配置（从配置文件和环境变量读取，或使用默认值）
func loadConfig() Config {
	// 默认配置
	config := Config{
		ServerAddr:    "0.0.0.0:8085",
		UploadDir:     "./uploads",
		PublicBaseURL: "http://localhost:8085",
	}

	// 尝试从配置文件读取
	if data, err := os.ReadFile("config.json"); err == nil {
		if err := json.Unmarshal(data, &config); err == nil {
			fmt.Println("已从配置文件加载配置")
		}
	}

	// 环境变量覆盖配置文件
	if value := os.Getenv("SERVER_ADDR"); value != "" {
		config.ServerAddr = value
	}
	if value := os.Getenv("UPLOAD_DIR"); value != "" {
		config.UploadDir = value
	}
	if value := os.Getenv("PUBLIC_BASE_URL"); value != "" {
		config.PublicBaseURL = value
	}

	return config
}

// 全局配置
var config Config

// 页面数据结构：用于向前端传递下载链接和二维码图片
type PageData struct {
	DownloadURL string // 下载链接
	FileID      string // 文件ID，用于生成二维码URL
}

func main() {
	// 加载配置
	config = loadConfig()

	// 初始化：创建上传目录（如果不存在）
	if err := os.MkdirAll(config.UploadDir, 0755); err != nil {
		fmt.Printf("创建上传目录失败：%v\n", err)
		return
	}

	// 注册路由
	http.HandleFunc("/", indexHandler)             // 首页（上传表单）
	http.HandleFunc("/upload", uploadHandler)      // 文件上传接口
	http.HandleFunc("/download/", downloadHandler) // 文件下载接口
	http.HandleFunc("/qr/", qrHandler)             // 二维码图片接口

	fmt.Printf("服务已启动：%s\n", config.ServerAddr)
	// 启动 Web 服务
	if err := http.ListenAndServe(config.ServerAddr, nil); err != nil {
		fmt.Printf("服务启动失败：%v\n", err)
	}
}

// 首页处理器：返回上传表单页面
func indexHandler(w http.ResponseWriter, r *http.Request) {
	// 现代风格的 HTML 上传表单
	html := `
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>文件上传工具</title>
    <style>
        :root {
            --primary-color: #3b82f6;
            --secondary-color: #64748b;
            --background-color: #f8fafc;
            --card-background: #ffffff;
            --text-color: #1e293b;
            --success-color: #10b981;
            --border-radius: 12px;
            --box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.1), 0 2px 4px -1px rgba(0, 0, 0, 0.06);
        }
        
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        body {
            font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background-color: var(--background-color);
            color: var(--text-color);
            min-height: 100vh;
            display: flex;
            flex-direction: column;
            align-items: center;
            padding: 2rem;
        }
        
        .container {
            max-width: 600px;
            width: 100%;
            margin: 0 auto;
        }
        
        header {
            text-align: center;
            margin-bottom: 3rem;
        }
        
        h1 {
            font-size: 2.5rem;
            font-weight: 700;
            color: var(--text-color);
            margin-bottom: 0.5rem;
        }
        
        .subtitle {
            color: var(--secondary-color);
            font-size: 1.1rem;
        }
        
        .card {
            background-color: var(--card-background);
            border-radius: var(--border-radius);
            box-shadow: var(--box-shadow);
            padding: 2rem;
            margin-bottom: 2rem;
        }
        
        .upload-form {
            display: flex;
            flex-direction: column;
            gap: 1.5rem;
        }
        
        .file-input-container {
            position: relative;
            border: 2px dashed #cbd5e1;
            border-radius: var(--border-radius);
            padding: 3rem;
            text-align: center;
            transition: all 0.3s ease;
        }
        
        .file-input-container:hover {
            border-color: var(--primary-color);
            background-color: rgba(59, 130, 246, 0.05);
        }
        
        .file-input {
            position: absolute;
            top: 0;
            left: 0;
            width: 100%;
            height: 100%;
            opacity: 0;
            cursor: pointer;
        }
        
        .file-input-label {
            display: flex;
            flex-direction: column;
            align-items: center;
            gap: 1rem;
        }
        
        .file-icon {
            font-size: 3rem;
            color: var(--secondary-color);
        }
        
        .file-input-text {
            font-size: 1.1rem;
            color: var(--secondary-color);
        }
        
        .file-input-hint {
            font-size: 0.9rem;
            color: #94a3b8;
        }
        
        .submit-btn {
            background-color: var(--primary-color);
            color: white;
            border: none;
            border-radius: var(--border-radius);
            padding: 1rem 2rem;
            font-size: 1.1rem;
            font-weight: 600;
            cursor: pointer;
            transition: all 0.3s ease;
        }
        
        .submit-btn:hover {
            background-color: #2563eb;
            transform: translateY(-2px);
            box-shadow: 0 10px 15px -3px rgba(59, 130, 246, 0.3);
        }
        
        .result {
            text-align: center;
        }
        
        .success-message {
            color: var(--success-color);
            font-size: 1.5rem;
            font-weight: 600;
            margin-bottom: 2rem;
        }
        
        .link-container {
            background-color: #f1f5f9;
            border-radius: var(--border-radius);
            padding: 1.5rem;
            margin-bottom: 2rem;
            text-align: left;
        }
        
        .link-label {
            font-size: 0.9rem;
            color: var(--secondary-color);
            margin-bottom: 0.5rem;
            font-weight: 500;
        }
        
        .link-box {
            width: 100%;
            padding: 0.75rem;
            border: 1px solid #e2e8f0;
            border-radius: 8px;
            font-size: 0.9rem;
            background-color: white;
            margin-bottom: 1rem;
        }
        
        .copy-btn {
            background-color: var(--secondary-color);
            color: white;
            border: none;
            border-radius: 8px;
            padding: 0.5rem 1rem;
            font-size: 0.9rem;
            cursor: pointer;
            transition: all 0.3s ease;
        }
        
        .copy-btn:hover {
            background-color: #475569;
        }
        
        .qr-code {
            margin: 2rem 0;
            padding: 1.5rem;
            background-color: white;
            border-radius: var(--border-radius);
            display: inline-block;
            box-shadow: var(--box-shadow);
        }
        
        .qr-code img {
            max-width: 200px;
            height: auto;
        }
        
        .qr-code-label {
            margin-bottom: 1rem;
            font-size: 1.1rem;
            font-weight: 500;
            color: var(--text-color);
        }
        
        .back-btn {
            background-color: var(--secondary-color);
            color: white;
            border: none;
            border-radius: var(--border-radius);
            padding: 0.75rem 1.5rem;
            font-size: 1rem;
            cursor: pointer;
            transition: all 0.3s ease;
            margin-top: 1rem;
        }
        
        .back-btn:hover {
            background-color: #475569;
        }
        
        .file-preview {
            margin-top: 1rem;
            padding: 1rem;
            background-color: #f1f5f9;
            border-radius: 8px;
        }
        
        .file-preview-info {
            display: flex;
            align-items: center;
            gap: 1rem;
        }
        
        .file-icon-large {
            font-size: 2rem;
        }
        
        .file-details {
            flex: 1;
        }
        
        .file-name {
            font-weight: 500;
            margin-bottom: 0.25rem;
            word-break: break-all;
        }
        
        .file-size {
            font-size: 0.9rem;
            color: var(--secondary-color);
        }
        
        .remove-file {
            background-color: #ef4444;
            color: white;
            border: none;
            border-radius: 4px;
            width: 28px;
            height: 28px;
            font-size: 0.8rem;
            cursor: pointer;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        
        @media (max-width: 768px) {
            body {
                padding: 1rem;
            }
            
            h1 {
                font-size: 2rem;
            }
            
            .card {
                padding: 1.5rem;
            }
            
            .file-input-container {
                padding: 2rem;
            }
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>文件上传工具</h1>
            <p class="subtitle">简单、快速地分享文件</p>
        </header>
        
        <div class="card">
            <form class="upload-form" action="/upload" method="post" enctype="multipart/form-data">
                <div class="file-input-container">
                    <input type="file" name="file" class="file-input" required>
                    <div class="file-input-label">
                        <div class="file-icon">📁</div>
                        <div class="file-input-text">点击或拖拽文件到此处</div>
                        <div class="file-input-hint">支持所有文件类型，最大 100MB</div>
                    </div>
                </div>
                
                <!-- 文件预览区域 -->
                <div class="file-preview" id="filePreview" style="display: none;">
                    <div class="file-preview-info">
                        <div class="file-icon-large">📄</div>
                        <div class="file-details">
                            <div class="file-name" id="fileName"></div>
                            <div class="file-size" id="fileSize"></div>
                        </div>
                        <button type="button" class="remove-file" onclick="removeFile()">✕</button>
                    </div>
                </div>
                
                <button type="submit" class="submit-btn">上传文件</button>
            </form>
        </div>

        {{if .DownloadURL}}
        <div class="card result">
            <div class="success-message">上传成功！</div>
            
            <div class="link-container">
                <div class="link-label">下载链接</div>
                <input type="text" value="{{.DownloadURL}}" class="link-box" id="downloadLink" readonly>
                <button class="copy-btn" onclick="copyLink()">复制链接</button>
            </div>
            
            <div class="qr-code-section">
                <div class="qr-code-label">扫码下载</div>
                <div class="qr-code">
                    <img src="/qr/{{.FileID}}" alt="下载二维码">
                </div>
            </div>
            
            <button class="back-btn" onclick="window.location.href='/'">返回上传</button>
        </div>
        {{end}}
    </div>

    <script>
        // 复制链接功能
        function copyLink() {
            const input = document.getElementById('downloadLink');
            input.select();
            document.execCommand('copy');
            alert('链接已复制！');
        }
        
        // 文件预览功能
        const fileInput = document.querySelector('.file-input');
        const filePreview = document.getElementById('filePreview');
        const fileName = document.getElementById('fileName');
        const fileSize = document.getElementById('fileSize');
        
        function formatFileSize(bytes) {
            if (bytes < 1024) return bytes + ' B';
            else if (bytes < 1048576) return (bytes / 1024).toFixed(2) + ' KB';
            else return (bytes / 1048576).toFixed(2) + ' MB';
        }
        
        function showFilePreview(file) {
            fileName.textContent = file.name;
            fileSize.textContent = formatFileSize(file.size);
            filePreview.style.display = 'block';
        }
        
        function hideFilePreview() {
            filePreview.style.display = 'none';
            fileName.textContent = '';
            fileSize.textContent = '';
        }
        
        // 文件选择事件
        fileInput.addEventListener('change', function(e) {
            if (e.target.files.length > 0) {
                showFilePreview(e.target.files[0]);
            } else {
                hideFilePreview();
            }
        });
        
        // 移除文件
        function removeFile() {
            fileInput.value = '';
            hideFilePreview();
        }
        
        // 拖拽功能
        const fileInputContainer = document.querySelector('.file-input-container');
        
        fileInputContainer.addEventListener('dragover', (e) => {
            e.preventDefault();
            fileInputContainer.style.borderColor = '#3b82f6';
            fileInputContainer.style.backgroundColor = 'rgba(59, 130, 246, 0.1)';
        });
        
        fileInputContainer.addEventListener('dragleave', () => {
            fileInputContainer.style.borderColor = '#cbd5e1';
            fileInputContainer.style.backgroundColor = 'transparent';
        });
        
        fileInputContainer.addEventListener('drop', (e) => {
            e.preventDefault();
            fileInputContainer.style.borderColor = '#cbd5e1';
            fileInputContainer.style.backgroundColor = 'transparent';
            
            if (e.dataTransfer.files.length > 0) {
                fileInput.files = e.dataTransfer.files;
                showFilePreview(e.dataTransfer.files[0]);
            }
        });
    </script>
</body>
</html>
	`
	// 解析并渲染模板（首次访问无数据，仅显示上传表单）
	tpl, err := template.New("index").Parse(html)
	if err != nil {
		http.Error(w, fmt.Sprintf("模板解析失败：%v", err), http.StatusInternalServerError)
		return
	}
	// 首次访问时 PageData 为空，仅显示上传表单
	tpl.Execute(w, PageData{})
}

// 文件上传处理器：接收文件、生成唯一标识、存储文件、返回下载链接和二维码
func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持 POST 方法", http.StatusMethodNotAllowed)
		return
	}

	// 解析上传的文件（限制文件大小为 100MB）
	r.ParseMultipartForm(100 << 20)
	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		http.Error(w, fmt.Sprintf("获取文件失败：%v", err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// 检查文件大小（限制为 100MB）
	if fileHeader.Size > 100<<20 {
		http.Error(w, "文件大小超过限制（100MB）", http.StatusBadRequest)
		return
	}

	// 生成唯一标识（UUID），保留原文件后缀（避免下载后无扩展名）
	fileExt := filepath.Ext(fileHeader.Filename)
	fileID := uuid.New().String()
	// 存储的文件名 = UUID + 原后缀
	saveFileName := fileID + fileExt
	savePath := filepath.Join(config.UploadDir, saveFileName)

	// 创建文件并写入内容
	outFile, err := os.Create(savePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("创建文件失败：%v", err), http.StatusInternalServerError)
		return
	}
	defer outFile.Close()

	// 复制上传的文件内容到本地文件
	_, err = io.Copy(outFile, file)
	if err != nil {
		http.Error(w, fmt.Sprintf("保存文件失败：%v", err), http.StatusInternalServerError)
		return
	}

	// 生成下载链接
	downloadURL := fmt.Sprintf("%s/download/%s", config.PublicBaseURL, fileID)

	// 生成二维码（Base64 格式，前端可直接显示）
	qrCodeBytes, err := qrcode.Encode(downloadURL, qrcode.Medium, 256)
	if err != nil {
		http.Error(w, fmt.Sprintf("生成二维码失败：%v", err), http.StatusInternalServerError)
		return
	}
	_ = qrCodeBytes // 二维码通过单独的API提供

	// 渲染页面，传递下载链接和二维码
	html := `
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>文件上传工具</title>
    <style>
        :root {
            --primary-color: #3b82f6;
            --secondary-color: #64748b;
            --background-color: #f8fafc;
            --card-background: #ffffff;
            --text-color: #1e293b;
            --success-color: #10b981;
            --border-radius: 12px;
            --box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.1), 0 2px 4px -1px rgba(0, 0, 0, 0.06);
        }
        
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        body {
            font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background-color: var(--background-color);
            color: var(--text-color);
            min-height: 100vh;
            display: flex;
            flex-direction: column;
            align-items: center;
            padding: 2rem;
        }
        
        .container {
            max-width: 600px;
            width: 100%;
            margin: 0 auto;
        }
        
        header {
            text-align: center;
            margin-bottom: 3rem;
        }
        
        h1 {
            font-size: 2.5rem;
            font-weight: 700;
            color: var(--text-color);
            margin-bottom: 0.5rem;
        }
        
        .subtitle {
            color: var(--secondary-color);
            font-size: 1.1rem;
        }
        
        .card {
            background-color: var(--card-background);
            border-radius: var(--border-radius);
            box-shadow: var(--box-shadow);
            padding: 2rem;
            margin-bottom: 2rem;
        }
        
        .upload-form {
            display: flex;
            flex-direction: column;
            gap: 1.5rem;
        }
        
        .file-input-container {
            position: relative;
            border: 2px dashed #cbd5e1;
            border-radius: var(--border-radius);
            padding: 3rem;
            text-align: center;
            transition: all 0.3s ease;
        }
        
        .file-input-container:hover {
            border-color: var(--primary-color);
            background-color: rgba(59, 130, 246, 0.05);
        }
        
        .file-input {
            position: absolute;
            top: 0;
            left: 0;
            width: 100%;
            height: 100%;
            opacity: 0;
            cursor: pointer;
        }
        
        .file-input-label {
            display: flex;
            flex-direction: column;
            align-items: center;
            gap: 1rem;
        }
        
        .file-icon {
            font-size: 3rem;
            color: var(--secondary-color);
        }
        
        .file-input-text {
            font-size: 1.1rem;
            color: var(--secondary-color);
        }
        
        .file-input-hint {
            font-size: 0.9rem;
            color: #94a3b8;
        }
        
        .submit-btn {
            background-color: var(--primary-color);
            color: white;
            border: none;
            border-radius: var(--border-radius);
            padding: 1rem 2rem;
            font-size: 1.1rem;
            font-weight: 600;
            cursor: pointer;
            transition: all 0.3s ease;
        }
        
        .submit-btn:hover {
            background-color: #2563eb;
            transform: translateY(-2px);
            box-shadow: 0 10px 15px -3px rgba(59, 130, 246, 0.3);
        }
        
        .result {
            text-align: center;
        }
        
        .success-message {
            color: var(--success-color);
            font-size: 1.5rem;
            font-weight: 600;
            margin-bottom: 2rem;
        }
        
        .link-container {
            background-color: #f1f5f9;
            border-radius: var(--border-radius);
            padding: 1.5rem;
            margin-bottom: 2rem;
            text-align: left;
        }
        
        .link-label {
            font-size: 0.9rem;
            color: var(--secondary-color);
            margin-bottom: 0.5rem;
            font-weight: 500;
        }
        
        .link-box {
            width: 100%;
            padding: 0.75rem;
            border: 1px solid #e2e8f0;
            border-radius: 8px;
            font-size: 0.9rem;
            background-color: white;
            margin-bottom: 1rem;
        }
        
        .copy-btn {
            background-color: var(--secondary-color);
            color: white;
            border: none;
            border-radius: 8px;
            padding: 0.5rem 1rem;
            font-size: 0.9rem;
            cursor: pointer;
            transition: all 0.3s ease;
        }
        
        .copy-btn:hover {
            background-color: #475569;
        }
        
        .qr-code {
            margin: 2rem 0;
            padding: 1.5rem;
            background-color: white;
            border-radius: var(--border-radius);
            display: inline-block;
            box-shadow: var(--box-shadow);
        }
        
        .qr-code img {
            max-width: 200px;
            height: auto;
        }
        
        .qr-code-label {
            margin-bottom: 1rem;
            font-size: 1.1rem;
            font-weight: 500;
            color: var(--text-color);
        }
        
        .back-btn {
            background-color: var(--secondary-color);
            color: white;
            border: none;
            border-radius: var(--border-radius);
            padding: 0.75rem 1.5rem;
            font-size: 1rem;
            cursor: pointer;
            transition: all 0.3s ease;
            margin-top: 1rem;
        }
        
        .back-btn:hover {
            background-color: #475569;
        }
        
        @media (max-width: 768px) {
            body {
                padding: 1rem;
            }
            
            h1 {
                font-size: 2rem;
            }
            
            .card {
                padding: 1.5rem;
            }
            
            .file-input-container {
                padding: 2rem;
            }
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>文件上传工具</h1>
            <p class="subtitle">简单、快速地分享文件</p>
        </header>
        
        <div class="card">
            <form class="upload-form" action="/upload" method="post" enctype="multipart/form-data">
                <div class="file-input-container">
                    <input type="file" name="file" class="file-input" required>
                    <div class="file-input-label">
                        <div class="file-icon">📁</div>
                        <div class="file-input-text">点击或拖拽文件到此处</div>
                        <div class="file-input-hint">支持所有文件类型，最大 100MB</div>
                    </div>
                </div>
                <button type="submit" class="submit-btn">开始上传</button>
            </form>
        </div>

        {{if .DownloadURL}}
        <div class="card result">
            <div class="success-message">上传成功！</div>
            
            <div class="link-container">
                <div class="link-label">下载链接</div>
                <input type="text" value="{{.DownloadURL}}" class="link-box" id="downloadLink" readonly>
                <button class="copy-btn" onclick="copyLink()">复制链接</button>
            </div>
            
            <div class="qr-code-section">
                <div class="qr-code-label">扫码下载</div>
                <div class="qr-code">
                    <img src="/qr/{{.FileID}}" alt="下载二维码">
                </div>
            </div>
            
            <button class="back-btn" onclick="window.location.href='/'">返回上传</button>
        </div>
        {{end}}
    </div>

    <script>
        // 复制链接功能
        function copyLink() {
            const input = document.getElementById('downloadLink');
            input.select();
            document.execCommand('copy');
            alert('链接已复制！');
        }
        
        // 拖拽功能
        const fileInputContainer = document.querySelector('.file-input-container');
        const fileInput = document.querySelector('.file-input');
        
        fileInputContainer.addEventListener('dragover', (e) => {
            e.preventDefault();
            fileInputContainer.style.borderColor = '#3b82f6';
            fileInputContainer.style.backgroundColor = 'rgba(59, 130, 246, 0.1)';
        });
        
        fileInputContainer.addEventListener('dragleave', () => {
            fileInputContainer.style.borderColor = '#cbd5e1';
            fileInputContainer.style.backgroundColor = 'transparent';
        });
        
        fileInputContainer.addEventListener('drop', (e) => {
            e.preventDefault();
            fileInputContainer.style.borderColor = '#cbd5e1';
            fileInputContainer.style.backgroundColor = 'transparent';
            
            if (e.dataTransfer.files.length > 0) {
                fileInput.files = e.dataTransfer.files;
            }
        });
    </script>
</body>
</html>
	`
	tpl, err := template.New("uploadResult").Parse(html)
	if err != nil {
		http.Error(w, fmt.Sprintf("模板解析失败：%v", err), http.StatusInternalServerError)
		return
	}
	tpl.Execute(w, PageData{
		DownloadURL: downloadURL,
		FileID:      fileID,
	})
}

// 文件下载处理器：根据文件 ID 查找文件并返回下载
func downloadHandler(w http.ResponseWriter, r *http.Request) {
	// 提取 URL 中的文件 ID（例如 /download/123456 → 123456）
	fileID := strings.TrimPrefix(r.URL.Path, "/download/")
	if fileID == "" {
		http.Error(w, "文件 ID 不能为空", http.StatusBadRequest)
		return
	}

	// 优化：直接尝试常见的文件扩展名，避免遍历整个目录
	// 常见文件扩展名列表
	extensions := []string{".jpg", ".jpeg", ".png", ".gif", ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".txt", ".zip", ".rar", ".7z", ".exe", ".mp3", ".mp4", ".avi", ".mov"}

	var targetFile string
	// 先尝试常见扩展名
	for _, ext := range extensions {
		tryPath := filepath.Join(config.UploadDir, fileID+ext)
		if _, err := os.Stat(tryPath); err == nil {
			targetFile = tryPath
			break
		}
	}

	// 如果常见扩展名没找到，再遍历目录（作为 fallback）
	if targetFile == "" {
		err := filepath.Walk(config.UploadDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && strings.HasPrefix(info.Name(), fileID) {
				targetFile = path
				return filepath.SkipAll // 找到后停止遍历
			}
			return nil
		})
		if err != nil || targetFile == "" {
			http.Error(w, "文件不存在", http.StatusNotFound)
			return
		}
	}

	// 获取文件名
	originalFileName := filepath.Base(targetFile)

	// 设置响应头，触发浏览器下载（而非直接打开）
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", originalFileName))
	w.Header().Set("Content-Type", "application/octet-stream")

	// 读取文件并写入响应
	http.ServeFile(w, r, targetFile)
}

// 二维码处理器：根据URL生成并返回二维码图片
func qrHandler(w http.ResponseWriter, r *http.Request) {
	// 提取 URL 参数
	fileID := strings.TrimPrefix(r.URL.Path, "/qr/")
	if fileID == "" {
		http.Error(w, "文件 ID 不能为空", http.StatusBadRequest)
		return
	}

	// 生成完整的下载链接
	downloadURL := fmt.Sprintf("%s/download/%s", config.PublicBaseURL, fileID)

	// 生成二维码
	qrCodeBytes, err := qrcode.Encode(downloadURL, qrcode.Medium, 256)
	if err != nil {
		http.Error(w, fmt.Sprintf("生成二维码失败：%v", err), http.StatusInternalServerError)
		return
	}

	// 设置响应头
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")

	// 返回二维码图片
	w.Write(qrCodeBytes)
}
