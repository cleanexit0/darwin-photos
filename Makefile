.PHONY: release-patch release-minor release-major

# Get current version, default to v0.0.0 if no tags exist
CURRENT_VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
VERSION_PARTS := $(subst ., ,$(subst v,,$(CURRENT_VERSION)))
MAJOR := $(word 1,$(VERSION_PARTS))
MINOR := $(word 2,$(VERSION_PARTS))
PATCH := $(word 3,$(VERSION_PARTS))

release-patch:
	@NEW_VERSION=v$(MAJOR).$(MINOR).$(shell echo $$(($(PATCH)+1))); \
	echo "Releasing $$NEW_VERSION (was $(CURRENT_VERSION))"; \
	git tag $$NEW_VERSION && git push origin $$NEW_VERSION

release-minor:
	@NEW_VERSION=v$(MAJOR).$(shell echo $$(($(MINOR)+1))).0; \
	echo "Releasing $$NEW_VERSION (was $(CURRENT_VERSION))"; \
	git tag $$NEW_VERSION && git push origin $$NEW_VERSION

release-major:
	@NEW_VERSION=v$(shell echo $$(($(MAJOR)+1))).0.0; \
	echo "Releasing $$NEW_VERSION (was $(CURRENT_VERSION))"; \
	git tag $$NEW_VERSION && git push origin $$NEW_VERSION
