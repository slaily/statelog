build:
	python -m build
# Verify package distribution
verify: build
	twine upload -r testpypi dist/*
# Publish package distribution to PyPI
publish-pypi: build
	twine upload -r pypi dist/*