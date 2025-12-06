# Suno 音乐格式（Music）

!!! note "请你注意"
    该接口 **非Suno官方的接口**，而是基于作者 **柏拉图** 的开源项目 [**Suno-API**](https://github.com/Suno-API/Suno-API) 实现的Suno代理接口。

    这里非常感谢作者的贡献，让我们可以方便使用Suno的强大功能，如果有时间，请给作者一个Star。

## 📝 简介 

Suno Music API 提供了一系列音乐生成和处理的功能，包括:

- 根据提示生成歌曲（灵感模式、自定义模式）

- 续写已有歌曲

- 拼接多个音频片段  

- 生成歌词

- 上传音频 

通过 API 可以方便地将 AI 音乐生成能力集成到你的应用中。

## 💡 请求示例

### 生成歌曲 ✅

```bash
curl --location 'https://你的newapi服务器地址/suno/submit/music' \
--header 'Authorization: Bearer $NEWAPI_API_KEY' \
--header 'Content-Type: application/json' \
--data '{
    "prompt":"[Verse]\nWalking down the streets\nBeneath the city lights\nNeon signs flickering\nLighting up the night\nHeart beating faster\nLike a drum in my chest\nI'\''m alive in this moment\nFeeling so blessed\n\nStilettos on the pavement\nStepping with grace\nSurrounded by the people\nMoving at their own pace\nThe rhythm of the city\nIt pulses in my veins\nLost in the energy\nAs my worries drain\n\n[Verse 2]\nConcrete jungle shining\nWith its dazzling glow\nEvery corner hiding secrets that only locals know\nA symphony of chaos\nBut it'\''s music to my ears\nThe hustle and the bustle\nWiping away my fears",
    "tags":"emotional punk",
    "mv":"chirp-v4",  
    "title":"City Lights"
}'
```

**响应示例:**

```json
{
  "code":"success",
  "message":"",
  "data":"736a6f88-bd29-4b1e-b110-37132a5325ac"
}
```

### 生成歌词 ✅

```bash
curl --location 'https://你的newapi服务器地址/suno/submit/lyrics' \
--header 'Authorization: Bearer $NEWAPI_API_KEY' \
--header 'Content-Type: application/json' \
--data '{
    "prompt":"dance"
}'
```

**响应示例:**

```json
{
  "code":"success",
  "message":"",
  "data":"736a6f88-bd29-4b1e-b110-37132a5325ac" 
}
```

### 上传音频 ❌

```bash
curl --location 'https://你的newapi服务器地址/suno/uploads/audio-url' \
--header 'Authorization: Bearer $NEWAPI_API_KEY' \  
--header 'Content-Type: application/json' \
--data '{ 
    "url":"http://cdnimg.example.com/ai/2024-06-18/d416d9c3c34eb22c7d8c094831d8dbd0.mp3"
}'
```

**响应示例:**

```json
{
  "code":"success",
  "message":"",
  "data":"736a6f88-bd29-4b1e-b110-37132a5325ac"
}  
```

### 歌曲拼接 ❌

```bash
curl --location 'https://你的newapi服务器地址/suno/submit/concat' \
--header 'Authorization: Bearer $NEWAPI_API_KEY' \
--header 'Content-Type: application/json' \  
--data '{
    "clip_id":"extend 后的 歌曲ID", 
    "is_infill": false
}'
```

**响应示例:**

```json
{
  "code":"success", 
  "message":"",
  "data":"736a6f88-bd29-4b1e-b110-37132a5325ac"  
}
```

### 查询任务状态 ✅

#### 批量查询

```bash
curl --location 'https://你的newapi服务器地址/suno/fetch' \
--header 'Authorization: Bearer $NEWAPI_API_KEY' \ 
--header 'Content-Type: application/json' \
--data '{
    "ids":["task_id"], 
    "action":"MUSIC"
}'  
```

**响应示例:**

```json
{
  "code":"success",
  "message":"", 
  "data":[
    {
      "task_id":"346c5d10-a4a1-4f49-a851-66a7dae6cfaf",
      "notify_hook":"",
      "action":"MUSIC", 
      "status":"IN_PROGRESS",
      "fail_reason":"",
      "submit_time":1716191749, 
      "start_time":1716191786,
      "finish_time":0,
      "progress":"0%",
      "data":[
        {
          "id":"e9893d04-6a63-4007-8473-64b706eca4d1",
          "title":"Electric Dance Party",
          "status":"streaming",
          "metadata":{
            "tags":"club banger high-energy edm",
            "prompt":"略",
            "duration":null,
            "error_type":null,
            "error_message":null, 
            "audio_prompt_id":null,
            "gpt_description_prompt":"miku dance"
          },
          "audio_url":"https://audiopipe.suno.ai/?item_id=e9893d04-6a63-4007-8473-64b706eca4d1",
          "image_url":"https://cdn1.suno.ai/image_e9893d04-6a63-4007-8473-64b706eca4d1.png",
          "video_url":"",
          "model_name":"chirp-v3", 
          "image_large_url":"https://cdn1.suno.ai/image_large_e9893d04-6a63-4007-8473-64b706eca4d1.png", 
          "major_model_version":"v3"
        }
      ]
    } 
  ] 
}
```

#### 单个查询

```bash
curl --location 'https://你的newapi服务器地址/suno/fetch/{{task_id}}' \ 
--header 'Authorization: Bearer $NEWAPI_API_KEY'
```

**响应示例:**

```json
{
  "code":"success",
  "message":"",
  "data":{
    "task_id":"f4a94d75-087b-4bb1-bd45-53ba293faf96",
    "notify_hook":"", 
    "action":"LYRICS",
    "status":"SUCCESS",
    "fail_reason":"",
    "submit_time":1716192124, 
    "start_time":1716192124, 
    "finish_time":1716192124,
    "progress":"100%", 
    "data":{
      "id":"f4a94d75-087b-4bb1-bd45-53ba293faf96",
      "text":"略", 
      "title":"Electric Fantasy",
      "status":"complete"  
    }
  }
}
```

## 📮 请求

所有请求都需在请求头中包含认证信息:

```
Authorization: Bearer $NEWAPI_API_KEY
```

### 端点

#### 生成歌曲
```
POST /suno/submit/music  
```
生成新的歌曲,支持灵感模式、自定义模式、续写。

#### 生成歌词
```
POST /suno/submit/lyrics
```
根据提示生成歌词。

#### 上传音频
```  
POST /suno/uploads/audio-url
```
上传音频文件。

#### 歌曲拼接  
```
POST /suno/submit/concat
```
将多个音频片段拼接为一首完整的歌曲。

#### 批量查询任务状态
```
POST /suno/fetch  
```
批量获取多个任务的状态和结果。

#### 查询单个任务状态
```
GET /suno/fetch/{{task_id}}
```  
查询单个任务的状态和结果。

### 请求体参数

#### 生成歌曲

##### `prompt`
- 类型:String
- 必需:灵感模式无需,自定义模式必需
- 说明:歌词内容,在自定义模式下需提供 

##### `mv`
- 类型:String  
- 必需:否
- 说明:模型版本,可选值:chirp-v3-0、chirp-v3-5,默认为 chirp-v3-0

##### `title` 
- 类型:String
- 必需:灵感模式无需,自定义模式必需  
- 说明:歌曲标题,在自定义模式下需提供

##### `tags`
- 类型:String
- 必需:灵感模式无需,自定义模式必需
- 说明:歌曲风格标签,使用逗号分隔,在自定义模式下需提供

##### `make_instrumental`
- 类型:Boolean 
- 必需:否
- 说明:是否生成纯音乐,true 为生成纯音乐  

##### `task_id`
- 类型:String
- 必需:续写时必需
- 说明:要续写的歌曲的任务 ID

##### `continue_at` 
- 类型:Float
- 必需:续写时必需
- 说明:从歌曲的第几秒开始续写  

##### `continue_clip_id`
- 类型:String 
- 必需:续写时必需
- 说明:要续写的歌曲的 clip ID

##### `gpt_description_prompt`
- 类型:String
- 必需:灵感模式必需,其他模式无需 
- 说明:灵感来源的文字描述

##### `notify_hook`
- 类型:String
- 必需:否 
- 说明:歌曲生成完成的回调通知地址

#### 生成歌词

##### `prompt` 
- 类型:String
- 必需:是
- 说明:歌词的主题或关键词

##### `notify_hook`
- 类型:String  
- 必需:否
- 说明:歌词生成完成的回调通知地址

#### 上传音频

##### `url`
- 类型:String
- 必需:是  
- 说明:要上传的音频文件的 URL 地址

#### 歌曲拼接

##### `clip_id` 
- 类型:String
- 必需:是
- 说明:要拼接的歌曲片段的 ID

##### `is_infill`
- 类型:Boolean
- 必需:否
- 说明:是否为填充模式  

#### 任务查询

##### `ids`
- 类型:String[]
- 必需:是
- 说明:要查询的任务 ID 列表

##### `action` 
- 类型:String 
- 必需:否
- 说明:任务类型,可选值:MUSIC、LYRICS

## 📥 响应

所有接口均返回统一的 JSON 格式响应:

```json
{
  "code":"success",
  "message":"",
  "data":"{{RESULT}}" 
}
```

### 成功响应

#### 基础响应字段

##### `code`
- 类型:String
- 说明:请求状态,success 为成功 

##### `message` 
- 类型:String
- 说明:请求失败时的错误信息

##### `data`
- 类型:根据不同接口而异
- 说明:请求成功时的返回数据
  - 生成歌曲、歌词、上传音频、歌曲拼接接口:返回任务 ID 字符串
  - 任务查询接口:返回任务对象或任务对象数组

#### 任务相关对象

##### 任务对象
###### `task_id`
- 类型:String  
- 说明:任务 ID

###### `notify_hook`
- 类型:String
- 说明:任务完成后的回调通知地址

###### `action`
- 类型:String
- 说明:任务类型,可选值:MUSIC、LYRICS  

###### `status` 
- 类型:String
- 说明:任务状态,可选值:IN_PROGRESS、SUCCESS、FAIL

###### `fail_reason` 
- 类型:String
- 说明:任务失败原因  

###### `submit_time`
- 类型:Integer
- 说明:任务提交时间戳

###### `start_time`
- 类型:Integer 
- 说明:任务开始时间戳

###### `finish_time`
- 类型:Integer
- 说明:任务结束时间戳 

###### `progress`
- 类型:String
- 说明:任务进度百分比

###### `data`
- 类型:根据任务类型不同而异 
- 说明:
  - 音乐生成任务:歌曲对象数组
  - 歌词生成任务:歌词对象  

##### 歌曲对象
###### `id`
- 类型:String
- 说明:歌曲 ID

###### `title`
- 类型:String
- 说明:歌曲标题

###### `status` 
- 类型:String
- 说明:歌曲状态 

###### `metadata`
- 类型:Object
- 说明:歌曲元数据
  - tags:歌曲风格标签
  - prompt:生成歌曲使用的歌词
  - duration:歌曲时长
  - error_type:错误类型
  - error_message:错误信息
  - audio_prompt_id:音频 prompt ID
  - gpt_description_prompt:灵感来源描述

###### `audio_url`
- 类型:String
- 说明:歌曲音频的 URL 地址

###### `image_url`
- 类型:String
- 说明:歌曲封面图的 URL 地址  

###### `video_url` 
- 类型:String
- 说明:歌曲视频的 URL 地址

###### `model_name`
- 类型:String
- 说明:生成歌曲使用的模型名称

###### `major_model_version`
- 类型:String 
- 说明:模型主版本号

##### 歌词对象
###### `id`
- 类型:String
- 说明:歌词 ID

###### `text`
- 类型:String 
- 说明:歌词内容

###### `title` 
- 类型:String
- 说明:歌词标题  

###### `status`
- 类型:String
- 说明:歌词状态

## 🌟 最佳实践

1. 提供尽量详细 、具体的歌曲或歌词生成提示,避免过于笼统或抽象

2. 查询任务状态时,轮询间隔建议为 2-5 秒,避免过于频繁

3. 灵感模式仅需提供 gpt_description_prompt 参数,API 会自动生成歌词、标题、标签等

4. 自定义模式需要提供 prompt、title、tags 参数,可以对歌曲有更多控制

5. 尽量使用最新版本的模型(如 chirp-v4),效果会更好

6. 使用回调通知功能(notify_hook 参数)可以降低轮询频率,提高效率

7. 音乐续写、拼接功能可以在原有音乐基础上,生成更加丰富、完整的作品

8. 注意处理可能出现的异常和错误,如网络超时、参数校验失败等