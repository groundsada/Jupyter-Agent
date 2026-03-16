import pytest

# Use anyio (already installed) as the async test runner.
# Mark all async tests with @pytest.mark.anyio or use the anyio fixture.
pytest_plugins = ("anyio",)
