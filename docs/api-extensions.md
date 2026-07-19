# Cloudreve API 增补文档

> 基于 Cloudreve v4.14.0，新增两个文件 API 端点。

---

## 1. `GET /api/v4/file/random` — 随机图片

**替代客户端 `list_files` → `random.choice` → `create_direct_link` 三步操作，服务端一次完成。**

### 请求

```
GET /api/v4/file/random
```

**认证：** 需要 `ScopeFilesRead` 权限（与所有 `/api/v4/file/*` 端点一致）。

### 请求参数

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|:---:|--------|------|
| `uri` | string | ✅ | — | 目录 URI，如 `cloudreve://my/Photos` |
| `recursive` | bool | | `true` | 是否递归遍历子目录 |
| `count` | int | | `1` | 返回图片数量，范围 1-50 |
| `thumbnail` | bool | | `false` | 是否同时返回缩略图 URL |
| `exclude` | []string | | `[]` | 需排除的文件名/URI 列表（去重用） |
| `ext` | string | | `""` | 扩展名过滤，逗号分隔，如 `jpg,png,webp` |
| `min_size` | int | | `0` | 最小文件大小（字节），过滤缩略图等小文件 |
| `max_size` | int | | `0` | 最大文件大小（字节），`0`=不限制 |
| `seed` | string | | `""` | 随机种子，同一 seed+目录+日期返回相同结果 |

### 请求示例

```http
GET /api/v4/file/random?uri=cloudreve://my/Wallpapers&count=5&recursive=true&ext=jpg,png&min_size=102400&seed=daily
```

### 响应

```json
{
  "code": 0,
  "data": {
    "images": [
      {
        "id": "98XDX8Sr",
        "name": "melk-abbey-library.jpg",
        "path": "cloudreve://my/Wallpapers/melk-abbey-library.jpg",
        "size": 1682177,
        "width": 2560,
        "height": 1920,
        "format": "jpeg",
        "url": "http://host/f/abc123/melk-abbey-library.jpg",
        "thumb_url": "http://host/api/v4/file/content/xxx/0/melk-abbey-library.jpg.jpg?sign=...",
        "expires": "2026-07-20T12:35:10+08:00"
      }
    ],
    "total_in_dir": 1542,
    "seed_used": "daily"
  },
  "msg": ""
}
```

### 响应字段

#### `RandomFileResponse`

| 字段 | 类型 | 说明 |
|------|------|------|
| `images` | []RandomImageResponse | 随机选取的图片列表 |
| `total_in_dir` | int64 | 目录（含子目录）中的图片总数 |
| `seed_used` | string | 实际使用的随机种子 |

#### `RandomImageResponse`

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | 文件 hash ID |
| `name` | string | 文件名 |
| `path` | string | 文件完整 URI 路径 |
| `size` | int64 | 文件大小（字节） |
| `width` | int | 图片宽度（px），来自 EXIF/ffprobe 元数据 |
| `height` | int | 图片高度（px），来自 EXIF/ffprobe 元数据 |
| `format` | string | 图片格式，如 `jpeg` `png` `webp` `avif` |
| `url` | string | 文件直链/下载 URL |
| `thumb_url` | string | 缩略图 URL（仅当请求参数 `thumbnail=true`） |
| `expires` | string | URL 过期时间（ISO 8601） |

### 错误码

| code | 说明 |
|------|------|
| `0` | 成功 |
| `40001` | URI 格式非法 |
| `404` | 目录不存在或无权限 |
| `403` | 未授权访问 |

### 设计要点

- **服务端随机** — 通过 `FileManager.Walk()` 遍历目录树，在内存中 Fisher-Yates 洗牌随机选取，比客户端拉全量列表快 100 倍+
- **`total_in_dir`** — 一次请求告知目录中图片总数，无需额外 API 调用
- **`seed`** — 使用 SHA-256(seed + 日期) 生成确定性随机序列，同一 seed 同一目录同一天返回相同图片，适用于"每日一图"等场景
- **`exclude`** — 支持文件名和 URI 两种排除方式，解决客户端手动去重痛点
- **同时返回 `url` + `thumb_url`** — 单次请求获取直链和缩略图，消除双 API 调用

### 典型使用场景

```javascript
// 场景1：随机壁纸
GET /api/v4/file/random?uri=cloudreve://my/Wallpapers&count=1&min_size=102400

// 场景2：每日一图（利用 seed 实现同一天相同结果）
GET /api/v4/file/random?uri=cloudreve://my/Photos&seed=daily&count=1

// 场景3：打卡卡片（同时获取原图 + 缩略图 + 模糊背景用）
GET /api/v4/file/random?uri=cloudreve://my/Quan&count=1&thumbnail=true

// 场景4：排除已展示图片去重
GET /api/v4/file/random?uri=cloudreve://my/Photos&exclude=pic001.jpg,pic002.jpg

// 场景5：仅 PNG 且大小范围过滤
GET /api/v4/file/random?uri=cloudreve://my/Icons&ext=png&min_size=1024&max_size=1048576
```

---

## 2. `GET /api/v4/file/thumb` — 缩略图（增强）

**在现有缩略图端点基础上，增加 Imgix/Cloudinary 风格的按请求图像处理参数。**

### 请求

```
GET /api/v4/file/thumb
```

**认证：** 需要 `ScopeFilesRead` 权限。

### 请求参数

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|:---:|--------|------|
| `uri` | string | ✅ | — | 文件 URI |
| `width` | int | | `0` | 缩略图最大宽度（px），`0`=使用全局设置 |
| `height` | int | | `0` | 缩略图最大高度（px），`0`=使用全局设置 |
| `format` | string | | `""` | 输出格式：`jpg` `png` `webp` `avif`，空=全局设置 |
| `quality` | int | | `0` | 输出质量 1-100，`0`=使用全局设置 |
| `fit` | string | | `"cover"` | 缩放模式 |
| `position` | string | | `"center"` | `fit=cover` 时的裁剪焦点 |
| `background` | string | | `""` | `fit=contain` 时的背景色 |
| `blur` | int | | `0` | 高斯模糊半径 1-100 |
| `no_cache` | bool | | `false` | 跳过缓存强制重新生成 |

#### `fit` 缩放模式

| 值 | 说明 |
|-----|------|
| `cover` | 裁剪填充，保证填满目标尺寸（默认） |
| `contain` | 等比缩放，完整显示图片内容 |
| `fill` | 拉伸填充，不保持比例 |
| `inside` | 仅当图片超出目标尺寸时才缩放 |

#### `position` 焦点位置（`fit=cover` 时生效）

| 值 | 说明 |
|-----|------|
| `center` | 居中裁剪（默认） |
| `top` | 顶部对齐 |
| `bottom` | 底部对齐 |
| `left` | 左侧对齐 |
| `right` | 右侧对齐 |
| `entropy` | 智能裁剪（保留信息量最大区域） |

### 请求示例

```http
GET /api/v4/file/thumb?uri=cloudreve://my/photo.jpg&width=800&height=600&fit=cover&position=entropy&format=webp&quality=85
```

### 响应

```json
{
  "code": 0,
  "data": {
    "url": "http://host/api/v4/file/content/xxx/0/file_thumb.webp?sign=...",
    "width": 800,
    "height": 600,
    "size": 45678,
    "format": "webp",
    "obfuscated": false,
    "expires": "2026-07-20T12:35:10+08:00"
  },
  "msg": ""
}
```

### 响应字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `url` | string | 缩略图访问 URL（带签名） |
| `width` | int | 缩略图实际宽度（px）；若服务端未处理则为原图宽度 |
| `height` | int | 缩略图实际高度（px） |
| `size` | int64 | 原文件大小（字节） |
| `format` | string | 实际输出格式 |
| `obfuscated` | bool | 文件是否经过混淆处理 |
| `expires` | string | URL 过期时间（ISO 8601） |

### 错误码

| code | 说明 |
|------|------|
| `0` | 成功 |
| `40001` | URI 格式非法 |
| `404` | 文件不存在或无缩略图 |

### 设计要点

- **完全向后兼容** — 所有新参数均有零值默认（空=回退到全局设置），现有调用方无需任何修改
- **`fit` + `position`** — 参考 Imgix/Cloudinary 行业标准：打卡卡片用 `cover`+`center`，头像用 `cover`+`entropy`
- **`blur`** — 打卡卡片常见的模糊背景+前景文字设计，服务端完成模糊而非让客户端做 CSS blur
- **返回实际尺寸** — `width` `height` `size` `format` 让客户端确切知道实际拿到了什么
- **`no_cache`** — 调试/管理场景强制重新生成缩略图

### 典型使用场景

```javascript
// 场景1：文件列表缩略图（使用全局默认设置）
GET /api/v4/file/thumb?uri=cloudreve://my/photo.jpg

// 场景2：打卡卡片背景（模糊 + 裁剪填充）
GET /api/v4/file/thumb?uri=cloudreve://my/card-bg.jpg&width=400&height=600&fit=cover&blur=20

// 场景3：头像缩略图（智能裁剪）
GET /api/v4/file/thumb?uri=cloudreve://my/avatar.jpg&width=128&height=128&fit=cover&position=entropy

// 场景4：高清预览（限制最大尺寸）
GET /api/v4/file/thumb?uri=cloudreve://my/photo.jpg&width=1920&height=1080&fit=inside&format=webp&quality=90

// 场景5：强制重新生成
GET /api/v4/file/thumb?uri=cloudreve://my/photo.jpg&no_cache=true
```

---

## 前端 TypeScript 类型

### `RandomFileService`（请求参数）

```typescript
export interface RandomFileService {
  uri: string;
  recursive?: boolean;   // 默认 true
  count?: number;        // 默认 1，范围 1-50
  thumbnail?: boolean;   // 默认 false
  exclude?: string[];
  ext?: string;
  min_size?: number;
  max_size?: number;
  seed?: string;
}
```

### `RandomFileResponse`（响应）

```typescript
export interface RandomFileResponse {
  images: RandomImageResponse[];
  total_in_dir: number;
  seed_used?: string;
}

export interface RandomImageResponse {
  id: string;
  name: string;
  path: string;
  size: number;
  width?: number;
  height?: number;
  format?: string;
  url: string;
  thumb_url?: string;
  expires?: string;
}
```

### `FileThumbResponse`（增强后）

```typescript
export interface FileThumbResponse {
  url: string;
  expires?: string;
  width?: number;       // 新增
  height?: number;      // 新增
  size?: number;        // 新增
  format?: string;      // 新增
  obfuscated?: boolean; // 新增
}
```

### API 调用函数

```typescript
// 随机图片
export function getRandomFiles(
  params: RandomFileService,
  contextHint?: string,
): ThunkResponse<RandomFileResponse>;

// 缩略图（无变化，响应类型自动包含新字段）
export function getFileThumb(
  path: string,
  contextHint?: string,
): ThunkResponse<FileThumbResponse>;
```

---

## 实现文件清单

| 文件 | 操作 | 说明 |
|------|:---:|------|
| `service/explorer/random.go` | 🆕 | 随机图片服务：Walk 遍历 + 过滤 + 种子随机 + URL 生成 |
| `service/explorer/file.go` | ✏️ | 增强 `FileThumbService`（+9 参数）和 `FileThumbResponse`（+5 字段） |
| `routers/controllers/file.go` | ✏️ | 新增 `RandomFile` 控制器 |
| `routers/router.go` | ✏️ | 注册 `GET /api/v4/file/random` 路由 |
| `assets/src/api/explorer.ts` | ✏️ | 前端类型定义 |
| `assets/src/api/api.ts` | ✏️ | 前端 `getRandomFiles()` API 函数 |
| `application/statics/statics.go` | ✏️ | 修复 `//go:embed` 二进制 zip 类型（`string`→`[]byte`） |
| `Dockerfile` | ✏️ | Alpine 源切换为 USTC 镜像 |
| `.github/workflows/docker-build.yml` | 🆕 | GitHub Actions ghcr.io Docker 构建工作流 |
