from __future__ import annotations

from dataclasses import dataclass, field
import queue
import threading
from typing import Any

from ..repositories import ConversationStore, UserStore


@dataclass
class RealtimeSubscription:
    owner_id: str
    events: queue.Queue[dict[str, Any]] = field(default_factory=lambda: queue.Queue(maxsize=100))
    closed: threading.Event = field(default_factory=threading.Event)


class RealtimeBroker:
    def __init__(self, store: ConversationStore, user_store: UserStore):
        self._store = store
        self._user_store = user_store
        self._lock = threading.Lock()
        self._subscriptions_by_owner: dict[str, list[RealtimeSubscription]] = {}

    @staticmethod
    def _normalize_limit(value: Any, default: int = 0) -> int:
        try:
            return max(0, int(value or default))
        except (TypeError, ValueError):
            return default

    def subscribe(
        self,
        owner_id: str,
        *,
        max_connections: int = 0,
        max_connections_per_user: int = 0,
        queue_size: int = 100,
    ) -> RealtimeSubscription:
        queue_size = max(1, self._normalize_limit(queue_size, 100))
        subscription = RealtimeSubscription(
            owner_id=owner_id,
            events=queue.Queue(maxsize=queue_size),
        )
        affected_owners: set[str] = set()
        with self._lock:
            affected_owners |= self._enforce_limits_locked(
                owner_id,
                max_connections=max_connections,
                max_connections_per_user=max_connections_per_user,
            )
            subscribers = self._subscriptions_by_owner.setdefault(owner_id, [])
            subscribers.append(subscription)
            affected_owners.add(owner_id)
        self._publish_connection_counts(affected_owners)
        return subscription

    def _enforce_limits_locked(
        self,
        owner_id: str,
        *,
        max_connections: int,
        max_connections_per_user: int,
    ) -> set[str]:
        affected_owners: set[str] = set()
        max_connections = self._normalize_limit(max_connections)
        max_connections_per_user = self._normalize_limit(max_connections_per_user)

        if max_connections_per_user > 0:
            subscribers = self._subscriptions_by_owner.get(owner_id, [])
            while len(subscribers) >= max_connections_per_user:
                oldest = subscribers.pop(0)
                affected_owners.add(owner_id)
                oldest.closed.set()
                self._force_event(oldest, {"type": "disconnect", "reason": "connection_limit"})
            if subscribers:
                self._subscriptions_by_owner[owner_id] = subscribers
            else:
                self._subscriptions_by_owner.pop(owner_id, None)

        if max_connections <= 0:
            return affected_owners
        while self._connection_count_locked() >= max_connections:
            oldest_owner = ""
            oldest_subscription: RealtimeSubscription | None = None
            for candidate_owner, subscribers in self._subscriptions_by_owner.items():
                if subscribers:
                    oldest_owner = candidate_owner
                    oldest_subscription = subscribers[0]
                    break
            if oldest_subscription is None:
                return affected_owners
            self._subscriptions_by_owner[oldest_owner].pop(0)
            if not self._subscriptions_by_owner[oldest_owner]:
                self._subscriptions_by_owner.pop(oldest_owner, None)
            affected_owners.add(oldest_owner)
            oldest_subscription.closed.set()
            self._force_event(oldest_subscription, {"type": "disconnect", "reason": "connection_limit"})
        return affected_owners

    def _connection_count_locked(self) -> int:
        return sum(len(items) for items in self._subscriptions_by_owner.values())

    def unsubscribe(self, subscription: RealtimeSubscription) -> None:
        affected_owner: str | None = None
        with self._lock:
            subscribers = self._subscriptions_by_owner.get(subscription.owner_id)
            if not subscribers:
                return
            self._subscriptions_by_owner[subscription.owner_id] = [
                item for item in subscribers if item is not subscription
            ]
            affected_owner = subscription.owner_id
            if not self._subscriptions_by_owner[subscription.owner_id]:
                self._subscriptions_by_owner.pop(subscription.owner_id, None)
        if affected_owner is not None:
            self._publish_connection_counts({affected_owner})

    def count_owner_connections(self, owner_id: str) -> int:
        with self._lock:
            return len(self._subscriptions_by_owner.get(owner_id, ()))

    def count_connections(self) -> int:
        with self._lock:
            return self._connection_count_locked()

    def publish_snapshot(self, owner_id: str) -> None:
        self._publish(owner_id, self.build_snapshot(owner_id))

    def publish_conversation_upsert(self, owner_id: str, conversation_id: str) -> None:
        conversation = self._store.get_conversation(conversation_id, owner_id)
        if conversation is None:
            self.publish_conversation_delete(owner_id, conversation_id)
            return
        try:
            messages = self._store.get_messages(conversation_id, owner_id)
        except ValueError:
            messages = []
        self._publish(
            owner_id,
            {
                "type": "conversation_upsert",
                "conversation": conversation.to_dict(),
                "messages": [message.to_dict() for message in messages],
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
            self._offer_event(subscription, event)

    def _publish_connection_counts(self, owner_ids: set[str]) -> None:
        for owner_id in owner_ids:
            self._publish(
                owner_id,
                {
                    "type": "connection_count",
                    "current_connection_count": self.count_owner_connections(owner_id),
                },
            )

    @staticmethod
    def _offer_event(subscription: RealtimeSubscription, event: dict[str, Any]) -> None:
        try:
            subscription.events.put_nowait(event)
            return
        except queue.Full:
            pass
        try:
            subscription.events.get_nowait()
        except queue.Empty:
            pass
        try:
            subscription.events.put_nowait(event)
        except queue.Full:
            subscription.closed.set()

    @staticmethod
    def _force_event(subscription: RealtimeSubscription, event: dict[str, Any]) -> None:
        while True:
            try:
                subscription.events.put_nowait(event)
                return
            except queue.Full:
                try:
                    subscription.events.get_nowait()
                except queue.Empty:
                    return

    def build_snapshot(
        self,
        owner_id: str,
    ) -> dict[str, Any]:
        return {
            "type": "snapshot",
            "conversations": [item.to_dict() for item in self._store.list_conversations(owner_id)],
        }
