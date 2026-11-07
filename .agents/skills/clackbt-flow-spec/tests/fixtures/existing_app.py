"""Illustrative existing app fixture for source-evidenced contract extraction.

This is a simple TUI model with text rendering and keyboard dispatch. It is
not executed by the contract extractor or validator.
"""
import json
from dataclasses import dataclass
from pathlib import Path


@dataclass
class Task:
    id: str
    title: str


class TaskRepository:
    def __init__(self, path: Path):
        self.path = path

    def list_tasks(self) -> list[Task]:
        return [Task(**row) for row in json.loads(self.path.read_text())]

    def delete(self, task_id: str) -> bool:
        if not task_id:
            raise ValueError("Select a task before deleting")
        before = self.list_tasks()
        remaining = [task for task in before if task.id != task_id]
        if len(remaining) == len(before):
            return False
        self.path.write_text(json.dumps([task.__dict__ for task in remaining]))
        return True


class TaskTui:
    def __init__(self, repo: TaskRepository):
        self.repo = repo
        self.screen = "task-list"
        self.selected_id = ""
        self.error_message = ""

    def render(self) -> str:
        if self.screen == "task-list":
            return "\n".join(f"{task.id}: {task.title}" for task in self.repo.list_tasks())
        return f"Task detail: {self.selected_id}\n[D]elete [Esc] Back"

    def on_key(self, key: str, task_id: str = "") -> str:
        if self.screen == "task-list" and key == "enter" and task_id:
            self.selected_id = task_id
            self.screen = "task-detail"
            return "task.open"
        if self.screen == "task-detail" and key == "escape":
            self.screen = "task-list"
            return "nav.back"
        if self.screen == "task-detail" and key.lower() == "d":
            return self.delete_current()
        return "noop"

    def delete_current(self) -> str:
        try:
            deleted = self.repo.delete(self.selected_id)
            if not deleted:
                self.error_message = "Task not found"
                return "task.delete.failed"
            self.screen = "task-list"
            self.selected_id = ""
            return "task.deleted"
        except (OSError, ValueError) as exc:
            self.error_message = str(exc)
            return "task.delete.failed"
