# ================= 配置区 =================
$ToolPath    = "D:\tools\svn-fucker.exe"
$SourceBase  = "D:\svn"
$TargetBase  = "D:\svn_code"
# ==========================================

# 检查工具与源路径是否存在
if (-not (Test-Path $ToolPath)) { Write-Error "找不到执行文件: $ToolPath"; exit }
if (-not (Test-Path $SourceBase)) { Write-Error "源路径不存在: $SourceBase"; exit }

Write-Host "[开始扫描] 源目录: $SourceBase" -ForegroundColor Cyan

# 遍历源目录下的所有文件夹（排除隐藏/系统文件夹可按需加 -Filter）
Get-ChildItem -Path $SourceBase -Directory | ForEach-Object {
    $FolderName = $_.Name
    $SrcPath    = Join-Path $SourceBase $FolderName
    $TgtPath    = Join-Path $TargetBase $FolderName

    Write-Host "`n[处理中] 文件夹: $FolderName" -ForegroundColor Yellow

    # 自动创建目标目录（避免工具因路径不存在报错）
    if (-not (Test-Path $TgtPath)) {
        New-Item -ItemType Directory -Path $TgtPath -Force | Out-Null
    }

    # 执行命令（双引号包裹路径，防止文件夹名带空格）
    & $ToolPath "$SrcPath" "$TgtPath"

    # 捕获退出码判断执行状态
    if ($LASTEXITCODE -eq 0) {
        Write-Host "[✔ 成功] $FolderName 处理完成" -ForegroundColor Green
    } else {
        Write-Warning "[✖ 失败] $FolderName 返回退出码: $LASTEXITCODE"
    }
    Write-Host "----------------------------------------"
}

Write-Host "[全部完成]" -ForegroundColor Magenta