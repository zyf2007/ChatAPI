from __future__ import annotations

from dataclasses import dataclass, field
import queue
import threading
from typing import Any

from ..repositories import ConversationStore


@dataclass
class RealtimeSubscription:
    owner_id: str
    events: queue.Queue[dict[str, Any]] = field(default_factory=queue.Queue)


class RealtimeBroker:
    def __init__(self, store: ConversationStore):
        self._store = store
        self._lock = threading.Lock()
        self._subscriptions_by_owner: dict[str, list[RealtimeSubscription]] = {}

    def subscribe(
        self,
        owner_id: str,
    ) -> RealtimeSubscription:
        subscription = RealtimeSubscription(owner_id=owner_id)
        with self._lock:
            subscribers = self._subscriptions_by_owner.setdefault(owner_id, [])
            subscribers.append(subscription)
        return subscription

    def unsubscribe(self, subscription: RealtimeSubscription) -> None:
        with self._lock:
            subscribers = self._subscriptions_by_owner.get(subscription.owner_id)
            if not subscribers:
                return
            self._subscriptions_by_owner[subscription.owner_id] = [
                item for item in subscribers if item is not subscription
            ]
            if not self._subscriptions_by_owner[subscription.owner_id]:
                self._subscriptions_by_owner.pop(subscription.owner_id, None)

    def publish_snapshot(self, owner_id: str) -> None:
        self._publish(owner_id, self.build_snapshot(owner_id))

    def publish_conversation_upsert(self, owner_id: str, conversation_id: str) -> None:
        conversation = self._store.get_conversation(conversation_id, owner_id)
        if conversation is None:
            self.publish_conversation_delete(owner_id, conversation_id)
            return
        try:
            messages = [
                item.to_dict()
                for item in self._store.get_messages(conversation_id, owner_id)
            ]
        except ValueError:
            messages = []
        self._publish(
            owner_id,
            {
                "type": "conversation_upsert",
                "conversation": conversation.to_dict(),
                "messages": messages,
            },
        )

    def publish_conversation_delete(self, owner_id: str, conversation_id: str) -> None:
        self._publish(
            owner_id,
            {
                "type": "conversation_delete",
                "conversation_id": conversation_id,
            },
        )

    def _publish(self, owner_id: str, event: dict[str, Any]) -> None:
        with self._lock:
            subscribers = tuple(self._subscriptions_by_owner.get(owner_id, ()))
        for subscription in subscribers:
            subscription.events.put_nowait(event)

    def build_snapshot(
        self,
        owner_id: str,
    ) -> dict[str, Any]:
        conversations = self._store.list_conversations(owner_id)
        messages_by_conversation: dict[str, list[dict[str, Any]]] = {}
        for conversation in conversations:
            try:
                messages_by_conversation[conversation.id] = [
                    item.to_dict()
                    for item in self._store.get_messages(
                        conversation.id,
                        owner_id,
                    )
                ]
            except ValueError:
                messages_by_conversation[conversation.id] = []

        return {
            "type": "snapshot",
            "conversations": [item.to_dict() for item in conversations],
            "messages_by_conversation": messages_by_conversation,
        }
