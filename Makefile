.PHONY: default test

default: local

# devops 测试环境
TEST_KODO_HOST = testing.kodo.cloudcare.cn
TEST_DOWNLOAD_ADDR = cloudcare-kodo.oss-cn-hangzhou.aliyuncs.com/corsair/test
TEST_SSL = 0
TEST_PORT = 80

# devops 预发环境
PREPROD_KODO_HOST = preprod-kodo.cloudcare.cn
PREPROD_DOWNLOAD_ADDR = cloudcare-kodo.oss-cn-hangzhou.aliyuncs.com/corsair/preprod
PREPROD_SSL = 1
PREPROD_PORT = 443

# alpha 环境
ALPHA_KODO_HOST = kodo-alpha.cloudcare.cn
ALPHA_DOWNLOAD_ADDR = cloudcare-kodo.oss-cn-hangzhou.aliyuncs.com/corsair/alpha
ALPHA_SSL = 1
ALPHA_PORT = 443

# 本地搭建的 kodo 测试(XXX: 自行绑定下这个域名到某个地址)
LOCAL_KODO_HOST = kodo-local.cloudcare.cn
LOCAL_DOWNLOAD_ADDR = cloudcare-kodo.oss-cn-hangzhou.aliyuncs.com/corsair/local
LOCAL_SSL = 0
LOCAL_PORT = 80

# 正式环境
RELEASE_KODO_HOST = kodo.cloudcare.cn
RELEASE_DOWNLOAD_ADDR = diaobaoyun-agent.oss-cn-hangzhou.aliyuncs.com/corsair/release
RELEASE_SSL = 1
RELEASE_PORT = 443

PUB_DIR = pub
BIN = corsair
NAME = corsair
ENTRY = main.go

VERSION := $(shell git describe --always --tags)

all: test release preprod local alpha

local:
	@echo "===== $(BIN) local ===="
	@rm -rf $(PUB_DIR)/local
	@mkdir -p build $(PUB_DIR)/local
	@mkdir -p git
	@echo 'package git; const (Sha1 string=""; BuildAt string=""; Version string=""; Golang string="")' > git/git.go
	@go run make.go -main $(ENTRY) -binary $(BIN) -name $(NAME) -build-dir build -archs "linux/amd64" -cgo \
		-kodo-host $(LOCAL_KODO_HOST) -download-addr $(LOCAL_DOWNLOAD_ADDR) -ssl $(LOCAL_SSL) -port $(LOCAL_PORT) \
		-release local -pub-dir $(PUB_DIR)
	@strip build/$(NAME)-linux-amd64/$(BIN)
	@tar czf $(PUB_DIR)/local/$(NAME)-$(VERSION).tar.gz -C build .
	tree -Csh $(PUB_DIR)

release:
	@echo "===== $(BIN) release ===="
	@rm -rf $(PUB_DIR)/release
	@mkdir -p build $(PUB_DIR)/release
	@mkdir -p git
	@echo 'package git; const (Sha1 string=""; BuildAt string=""; Version string=""; Golang string="")' > git/git.go
	@go run make.go -main $(ENTRY) -binary $(BIN) -name $(NAME) -build-dir build -archs "linux/amd64" -cgo \
		-kodo-host $(RELEASE_KODO_HOST) -download-addr $(RELEASE_DOWNLOAD_ADDR) -ssl $(RELEASE_SSL) -port $(RELEASE_PORT) \
		-release release -pub-dir $(PUB_DIR)
	@strip build/$(NAME)-linux-amd64/$(BIN)
	@tar czf $(PUB_DIR)/release/$(NAME)-$(VERSION).tar.gz -C build .
	tree -Csh $(PUB_DIR)

test:
	@echo "===== build test ===="
	@rm -rf $(PUB_DIR)/test
	@mkdir -p build $(PUB_DIR)/test
	@mkdir -p git
	@echo $'package git;\n const (Sha1 string=""; BuildAt string=""; Version string=""; Golang string="")' > git/git.go
	#@go run make.go -main $(ENTRY) -binary $(BIN) -name $(NAME) -build-dir build -archs "linux/amd64" -cgo \
		-kodo-host $(TEST_KODO_HOST) -download-addr $(TEST_DOWNLOAD_ADDR) -ssl $(TEST_SSL) -port $(TEST_PORT) \
		-release test -pub-dir $(PUB_DIR)
	#@strip build/$(NAME)-linux-amd64/$(BIN)
	#@tar czf $(PUB_DIR)/test/$(NAME)-$(VERSION).tar.gz -C build .
	#tree -Csh $(PUB_DIR)

pub_local:
	@echo "publish local agent ..."
	@go run make.go -pub -release local -pub-dir $(PUB_DIR) -name $(NAME)

pub_test:
	@echo "publish test agent ..."
	@go run make.go -pub -release test -pub-dir $(PUB_DIR) -name $(NAME)

pub_release:
	@echo "publish release agent ..."
	@go run make.go -pub -release release -pub-dir $(PUB_DIR) -name $(NAME)

clean:
	rm -rf build/*
	rm -rf $(PUB_DIR)/*
