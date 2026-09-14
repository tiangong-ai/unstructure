---
docType: guide
scope: repo
status: legacy
authoritative: false
owner: unstructure
language: en
whenToUse: "When checking legacy setup notes; verify commands against current files before running."
whenToUpdate: "When curating or deleting legacy setup and operation notes. Authoritative operation guidance belongs in AGENTS.md and _docs/runbooks/development.md."
checkPaths:
  - AGENTS.md
  - .docpact/config.yaml
  - requirements.txt
  - src/**
  - docker/**
lastReviewedAt: 2026-09-14
lastReviewedCommit: 58f7612bb02891f9db5c153f0cc324f1d3f95f9b
---

# TianGong AI Unstructure

<!-- tiangong-ai-migration-20260914:start -->

## GitHub organization migration / GitHub 组织迁移

This original repository now belongs to the [tiangong-ai organization](https://github.com/tiangong-ai), with its repository identity and history retained. The CLI package `@tiangong-ai/cli` and command `tiangong-ai` are unchanged. Wiki is now published as `@tiangong-ai/wiki`; its commands remain unchanged. See the [migration and upgrade notes](https://github.com/tiangong-ai/cli-toolkit/releases/tag/v0.0.63).

该仓库已迁入 [tiangong-ai 组织](https://github.com/tiangong-ai)，仓库身份与历史保留。CLI 包名 `@tiangong-ai/cli` 和命令 `tiangong-ai` 不变；Wiki 新包名为 `@tiangong-ai/wiki`，命令不变。升级方式见[迁移说明](https://github.com/tiangong-ai/cli-toolkit/releases/tag/v0.0.63)。原个人账号 `tiangong-ai-legacy` 保留历史；请自行 Follow 新组织。

<!-- tiangong-ai-migration-20260914:end -->

## Env Preparing

### Using VSCode Dev Contariners

[Tutorial](https://code.visualstudio.com/docs/devcontainers/tutorial)

Python 3 -> Additional Options -> 3.12-bullseye -> ZSH Plugins (Last One) -> Trust @devcontainers-contrib -> Keep Defaults

Setup `venv`:

```bash
python3.12 -m venv .venv
source .venv/bin/activate
```

Install requirements:

```bash
python.exe -m pip install --upgrade pip

pip install --upgrade pip

pip install --upgrade pip -i https://pypi.tuna.tsinghua.edu.cn/simple
pip install -r requirements.txt -i https://pypi.tuna.tsinghua.edu.cn/simple
pip install -r requirements.txt --upgrade
```

```bash
sudo apt update
sudo apt install python3.12-dev
sudo apt install -y libmagic-dev
sudo apt install -y poppler-utils
sudo apt install -y libreoffice
sudo apt install -y pandoc
```

Test Cuda (optional):

```bash
nvidia-smi
```

### Auto Build

The auto build will be triggered by pushing any tag named like release-v$version. For instance, push a tag named as v0.0.1 will build a docker image of 0.0.1 version.

```bash
#list existing tags
git tag
#creat a new tag
git tag v0.0.1
#push this tag to origin
git push origin v0.0.1
```

### sphinx

```bash
sphinx-apidoc --force -o sphinx/source/ src/
sphinx-autobuild sphinx/source docs/
```

### Docker Manually Build

```bash
docker build -t linancn/tiangong-ai-unstructure:v0.0.1 .
docker push linancn/tiangong-ai-unstructure:v0.0.1
```

### Nginx config

default file location: /etc/nginx/sites-enabled/default

```bash
sudo apt update
sudo apt install nginx
sudo nginx
sudo nginx -s reload
sudo nginx -s stop
```

## Update the verison of tesseract in WSL Shell
### remove the old version and add the necessary libraries
```bash
sudo apt-get remove tesseract-ocr
sudo apt-get install libpng-dev libjpeg-dev libtiff-dev libgif-dev libwebp-dev libopenjp2-7-dev zlib1g-dev
```
### get the latest version of leptonica by running the following code in sequence 
```bash
cd
wget https://github.com/DanBloomberg/leptonica/archive/refs/tags/1.84.1.tar.gz
tar -xzvf 1.84.1.tar.gz
cd leptonica-1.84.1
mkdir build
cd build
sudo snap install cmake # get the cmake version 3.28.3 #
cmake ..
make -j`nproc`
sudo make install
```
### get the latest version of tesseract by running the following code in sequence 
``` bash
cd
wget https://github.com/tesseract-ocr/tesseract/archive/refs/tags/5.3.4.tar.gz
tar -xzvf 5.3.4.tar.gz
cd tesseract-5.3.4
mkdir build
cd build
cmake ..
make -j `nproc`
sudo make install
```
### set environment variables
``` bash
cd
nano ~/.bashrc
```
#### add the following content at the end of the file，save the file(Ctrl-O) and exit(Ctrl-X)
export TESSDATA_PREFIX=/usr/local/share/tessdata
#### activate the settting
``` bash
source ~/.bashrc
```
### get language models
https://github.com/tesseract-ocr/tessdata/blob/main/chi_sim.traineddata
https://github.com/tesseract-ocr/tessdata/blob/main/chi_tra.traineddata
https://github.com/tesseract-ocr/tessdata/blob/main/eng.traineddata

/usr/local/share/tessdata/

### check the language models currently in use
``` bash
tesseract --list-langs
```

## Run in Background
```bash
watch -n 1 nvidia-smi
#统计文件夹中的文件个数
find test/queue/pickle/ -type f | wc -l
ls -lt processed_docs/esg_txt/ | head -n 10

nohup .venv/bin/python3.12 src/journals/chunk_by_title_sci.py > log.txt 2>&1 &
pkill -f src/journals/chunk_by_title_sci.py

CUDA_VISIBLE_DEVICES=2 nohup .venv/bin/python3.12 src/esg/1_chunk_by_title.py > esg_unstructured.log 2>&1 &
CUDA_VISIBLE_DEVICES=2 nohup .venv/bin/python3.12 src/esg/3_chunk_by_title_pages.py > esg_meta_unstructured.log 2>&1 &

CUDA_VISIBLE_DEVICES=0 nohup .venv/bin/python3.12 src/esg/1_chunk_by_title_0.py > esg_unstructured_0.log 2>&1 &
CUDA_VISIBLE_DEVICES=1 nohup .venv/bin/python3.12 src/esg/1_chunk_by_title_1.py > esg_unstructured_1.log 2>&1 &
CUDA_VISIBLE_DEVICES=2 nohup .venv/bin/python3.12 src/esg/1_chunk_by_title_2.py > esg_unstructured_2.log 2>&1 &
CUDA_VISIBLE_DEVICES=3 nohup .venv/bin/python3.12 src/esg/1_chunk_by_title_3.py > esg_unstructured_3.log 2>&1 &

pkill -f src/esg/1_chunk_by_title_0.py
pkill -f src/esg/1_chunk_by_title_1.py
pkill -f src/esg/1_chunk_by_title_2.py
pkill -f src/esg/1_chunk_by_title_3.py



nohup .venv/bin/python3.12 src/esg/2_embedding_init.py > esg_embedding_log.txt 2>&1 &

nohup .venv/bin/python3.12 src/standards/1_chunk_by_title.py > log.txt 2>&1 &

nohup .venv/bin/python3.12 src/reports/1_chunk_by_title.py > log.txt 2>&1 &
nohup .venv/bin/python3.12 src/reports/2_embedding_init.py > log.txt 2>&1 &

nohup .venv/bin/python3.12 src/education/4_pickle_to_pinecone.py > log.txt 2>&1 &

##standards
nohup .venv/bin/python3 src/standards/2_pickle_to_opensearch.py > standard_opensearch_log.txt 2>&1 &
nohup .venv/bin/python3 src/standards/3_pickle_to_pinecone.py > standard_pinecone_log.txt 2>&1 &

##reports
nohup .venv/bin/python3.12 src/reports/3_pickle_to_pinecone.py &
nohup .venv/bin/python3.12 src/reports/3_pickle_to_opensearch.py &
nohup .venv/bin/python3 src/reports/3_pickle_to_opensearch_aws.py > report_opensearch_aws_log.txt 2>&1 &
pkill -f src/reports/3_pickle_to_opensearch.py
pkill -f src/reports/3_pickle_to_opensearch_aws.py


##esg
nohup .venv/bin/python3.12 src/esg/3_pickle_to_opensearch.py > esg_opensearch_log.txt 2>&1 &
nohup .venv/bin/python3.12 src/esg/3_pickle_to_pinecone.py > esg_pinecone_log.txt 2>&1 &
nohup .venv/bin/python3 src/esg/3_pickle_to_opensearch_aws.py > esg_opensearch_aws_log.txt 2>&1 &
nohup .venv/bin/python3 src/esg/3_pickle_to_pinecone_aws.py > esg_pinecone_aws_log.txt 2>&1 &

pkill -f src/esg/3_pickle_to_pinecone.py
pkill -f src/esg/3_pickle_to_opensearch.py


##journal
CUDA_VISIBLE_DEVICES=0 nohup .venv/bin/python3.12 src/journals/chunk_by_title_sci_0.py > journal_pinecone_0.log 2>&1 &
CUDA_VISIBLE_DEVICES=1 nohup .venv/bin/python3.12 src/journals/chunk_by_title_sci_1.py > journal_pinecone_1.log 2>&1 &
CUDA_VISIBLE_DEVICES=2 nohup .venv/bin/python3.12 src/journals/chunk_by_title_sci_2.py > journal_pinecone_2.log 2>&1 &
CUDA_VISIBLE_DEVICES=3 nohup .venv/bin/python3.12 src/journals/chunk_by_title_sci_3.py > journal_pinecone_3.log 2>&1 &

pkill -f src/journals/chunk_by_title_sci_0.py
pkill -f src/journals/chunk_by_title_sci_1.py
pkill -f src/journals/chunk_by_title_sci_2.py
pkill -f src/journals/chunk_by_title_sci_3.py


find test/queue/pdf/ -type f | wc -l
find processed_docs/journal_pickle/ -maxdepth 1 -type f | wc -l
find processed_docs/journal_pickle/ -type f -exec ls -lS {} + | sort -n -k 5 | head -n 10
ls -ltR processed_docs/journal_pickle/ | head -n 10

find docs/processed_docs/journal_new_pickle/ -type f | wc -l

nohup .venv/bin/python3 src/journals/2_pickle_to_pinecone_aws.py > journal_pinecone_aws_Oct31_log.txt 2>&1 &

nohup .venv/bin/python3 src/patents/1_pickle_2_pinecone.py > patents_2_pinecone_log.txt 2>&1 &
nohup .venv/bin/python3 src/patents/1_pickle_2_opensearch_aws.py > patents_2_opensearch_log.txt 2>&1 &

nohup .venv/bin/python3 src/edu_textbooks/pickle_to_pinecone_aws.py > textbook_pinecone_log.txt 2>&1 &
nohup .venv/bin/python3 src/edu_textbooks/pickle_to_opensearch_aws.py > textbook_opensearch_log.txt 2>&1 &


nohup .venv/bin/python3.12 src/education/0_pdf_pickle_service_0.py 2>&1 &
nohup .venv/bin/python3.12 src/education/0_pdf_pickle_service_1.py 2>&1 &
nohup .venv/bin/python3.12 src/education/0_pdf_pickle_service_2.py 2>&1 &


nohup .venv/bin/python3 src/standards/1_file2pickle.py > standard_pickle_log.txt 2>&1 &

nohup .venv/bin/python3 src/esg/1_file2pickle.py > esg_pickle_log.txt 2>&1 &

nohup .venv/bin/python3 src/ali/3_pickle_to_opensearch.py > epr_log.txt 2>&1 &
nohup .venv/bin/python3 src/ali/3_pickle_to_pinecone.py > epr_log_pinecone.txt 2>&1 &

nohup .venv/bin/python3 src/journals/file_to_pickle1.py > redo1.log 2>&1 &
nohup .venv/bin/python3 src/journals/file_to_pickle2.py > redo2.log 2>&1 &
nohup .venv/bin/python3 src/journals/file_to_pickle3.py > redo3.log 2>&1 &
nohup .venv/bin/python3 src/journals/file_to_pickle4.py > redo4.log 2>&1 &
nohup .venv/bin/python3 src/journals/file_to_pickle5.py > redo5.log 2>&1 &
nohup .venv/bin/python3 src/journals/file_to_pickle6.py > redo6.log 2>&1 &
nohup .venv/bin/python3 src/journals/file_to_pickle7.py > redo7.log 2>&1 &
nohup .venv/bin/python3 src/journals/file_to_pickle8.py > redo8.log 2>&1 &
nohup .venv/bin/python3 src/esg/two_stage_enqueue.py > enqueue.log 2>&1 &
nohup .venv/bin/python3 src/journals/two_stage_enqueue.py > journal_enqueue.log 2>&1 & # test file
nohup .venv/bin/python3 src/journals/two_stage_enqueue_urgent.py > enqueue_urgent.log 2>&1 & 


#同一个log，不记录顺序：
# normal（默认目录）
nohup .venv/bin/python3 src/journals/two_stage_enqueue.py > enqueue.normal.log 2>&1 &

# urgent（随时加塞）
TWO_STAGE_INPUT_DIR=test/journal-test/urgent \
TWO_STAGE_OUTPUT_DIR=test/journal-test/pickle-urg \
TWO_STAGE_PRIORITY=urgent \
nohup .venv/bin/python3 src/journals/two_stage_enqueue.py > enqueue.urgent.log 2>&1 &




# 重启Redis（未尝试，慎用）
# Linux 环境
sudo service redis-server restart
sudo systemctl restart redis
redis-cli save && sudo systemctl restart redis
redis-cli flushall  # 一键清空所有数据，谨慎使用

# Docker 环境
docker restart redis
docker exec redis redis-cli save && docker restart redis #清理缓存并重启

# 测试重启成功
redis-cli ping # 应该返回PONG
redis-cli dbsize #查看当前数据库中的键数量





# 两个队列同时启动，观察顺序

# normal：在 normal 目录里启动
( cd test/journal-test/normal && \
  nohup ../../.venv/bin/python3 ../../test/journal-test/two_stage_enqueue.py \
> enqueue.normal.log 2>&1 & )

# urgent：在 urgent 目录里启动 随时加塞
( cd test/journal-test/urgent &&  TWO_STAGE_INPUT_DIR=.  TWO_STAGE_OUTPUT_DIR=..pickle-urg \
  nohup ../../.venv/bin/python3 ../../test/journal-test/two_stage_enqueue.py \
  > enqueue.urgent.log 2>&1 & )

# 合并查看处理顺序（带队列标识+时间戳）
( tail -F test/journal-test/normal/celery_two_stage.log | sed 's/^/[normal] /' & \
  tail -F test/journal-test/urgent/celery_two_stage.log | sed 's/^/[urgent] /' ) \
| awk '{print strftime("%F %T"), $0; fflush();}'

# 记录合并结果
nohup bash -c '( tail -F test/journal-test/normal/celery_two_stage.log | sed "s/^/[normal] /" & \
  tail -F test/journal-test/urgent/celery_two_stage.log | sed "s/^/[urgent] /" ) \
| awk '"'"'{print strftime("%F %T"), $0; fflush();}'"'"'' \
> two_stage_merged.log 2>&1 &



pkill -f "file_to_pickle1.py"
pkill -f "file_to_pickle2.py"
pkill -f "file_to_pickle3.py"
pkill -f "file_to_pickle4.py"
pkill -f "file_to_pickle5.py"
pkill -f "file_to_pickle6.py"
pkill -f "file_to_pickle7.py"
pkill -f "file_to_pickle8.py"


```
