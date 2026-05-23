from __future__ import annotations

from dataclasses import dataclass, field
import queue
import threading
import uuid
from typing import Any, Callable

from ..repositories import ConversationStore, UserStore


@dataclass
class RealtimeSubscription:
    owner_id: str
    events: queue.Queue[dict[str, Any]] = field(default_factory=lambda: queue.Queue(maxsize=100))
    closed: threading.Event = field(default_factory=threading.Event)


@dataclass
class ActiveConnection:
    connection_id: str
    owner_id: str
    kind: str
    close_callback: Callable[[], None] | None = None


@dataclass(frozen=True)
class ConnectionLease:
    connection_id: str
    owner_id: str
    kind: str


class ConnectionLimitExceeded(RuntimeError):
    pass


class RealtimeBroker:
    def __init__(self, store: ConversationStore, user_store: UserStore):
        self._store = store
        self._user_store = user_store
        self._lock = threading.Lock()
        self._subscriptions_by_owner: dict[str, list[RealtimeSubscription]] = {}
        self._connections_by_owner: dict[str, list[ActiveConnection]] = {}

    @staticmethod
    def _normalize_limit(value: Any, default: int = 0) -> int:
        try:
            return max(0, int(value or default))
        except (TypeError, ValueError):
            return default

    def acquire_connection(
        self,
        owner_id: str,
        *,
        kind: str,
        max_connections: int = 0,
        max_connections_per_user: int = 0,
        close_callback: Callable[[], None] | None = None,
    ) -> ConnectionLease:
        connection = ActiveConnection(
            connection_id=uuid.uuid4().hex,
            owner_id=owner_id,
            kind=kind,
            close_callback=close_callback,
        )
        affected_owners: set[str] = set()
        with self._lock:
            affected_owners |= self._enforce_limits_locked(
                owner_id,
                max_connections=max_connections,
                max_connections_per_user=max_connections_per_user,
            )
            if self._would_exceed_limits_locked(
                owner_id,
                max_connections=max_connections,
                max_connections_per_user=max_connections_per_user,
            ):
                raise ConnectionLimitExceeded("connection limit exceeded")
            self._connections_by_owner.setdefault(owner_id, []).append(connection)
            affected_owners.add(owner_id)
        self._publish_connection_counts(affected_owners)
        return ConnectionLease(
            connection_id=connection.connection_id,
            owner_id=owner_id,
            kind=kind,
        )

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
        lease = self.acquire_connection(
            owner_id,
            kind="websocket",
            max_connections=max_connections,
            max_connections_per_user=max_connections_per_user,
            close_callback=subscription.closed.set,
        )
        setattr(subscription, "_connection_lease", lease)
        with self._lock:
            subscribers = self._subscriptions_by_owner.setdefault(owner_id, [])
            subscribers.append(subscription)
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
            while self._owner_connection_count_locked(owner_id) >= max_connections_per_user:
                removed = self._drop_oldest_connection_locked(owner_id=owner_id)
                if removed is None:
                    break
                affected_owners.add(removed.owner_id)

        if max_connections <= 0:
            return affected_owners
        while self._connection_count_locked() >= max_connections:
            removed = self._drop_oldest_connection_locked()
            if removed is None:
                return affected_owners
            affected_owners.add(removed.owner_id)
        return affected_owners

    def _connection_count_locked(self) -> int:
        return sum(len(items) for items in self._connections_by_owner.values())

    def _owner_connection_count_locked(self, owner_id: str) -> int:
        return len(self._connections_by_owner.get(owner_id, ()))

    def _would_exceed_limits_locked(
        self,
        owner_id: str,
        *,
        max_connections: int,
        max_connections_per_user: int,
    ) -> bool:
        max_connections = self._normalize_limit(max_connections)
        max_connections_per_user = self._normalize_limit(max_connections_per_user)
        if max_connections_per_user > 0 and self._owner_connection_count_locked(owner_id) >= max_connections_per_user:
            return True
        if max_connections > 0 and self._connection_count_locked() >= max_connections:
            return True
        return False

    def _drop_oldest_connection_locked(self, owner_id: str | None = None) -> ActiveConnection | None:
        candidate_owners = [owner_id] if owner_id is not None else list(self._connections_by_owner.keys())
        for candidate_owner in candidate_owners:
            connections = self._connections_by_owner.get(candidate_owner, [])
            for index, connection in enumerate(connections):
                if connection.close_callback is None:
                    continue
                connections.pop(index)
                if connections:
                    self._connections_by_owner[candidate_owner] = connections
                else:
                    self._connections_by_owner.pop(candidate_owner, None)
                self._close_connection_locked(connection)
                return connection
        return None

    def _close_connection_locked(self, connection: ActiveConnection) -> None:
        self._remove_subscription_for_connection_locked(connection.connection_id)
        if connection.close_callback is not None:
            connection.close_callback()

    def _remove_subscription_for_connection_locked(self, connection_id: str) -> None:
        for owner_id, subscribers in list(self._subscriptions_by_owner.items()):
            remaining = [
                subscription
                for subscription in subscribers
                if getattr(getattr(subscription, "_connection_lease", None), "connection_id", None) != connection_id
            ]
            if len(remaining) == len(subscribers):
                continue
            if remaining:
                self._subscriptions_by_owner[owner_id] = remaining
            else:
                self._subscriptions_by_owner.pop(owner_id, None)
            return

    def unsubscribe(self, subscription: RealtimeSubscription) -> None:
        lease = getattr(subscription, "_connection_lease", None)
        if isinstance(lease, ConnectionLease):
            self.release_connection(lease)
            return
        with self._lock:
            subscribers = self._subscriptions_by_owner.get(subscription.owner_id)
            if not subscribers:
                return
            self._subscriptions_by_owner[subscription.owner_id] = [
                item for item in subscribers if item is not subscription
            ]
            if not self._subscriptions_by_owner[subscription.owner_id]:
                self._subscriptions_by_owner.pop(subscription.owner_id, None)

    def release_connection(self, lease: ConnectionLease) -> None:
        affected_owner: str | None = None
        with self._lock:
            connections = self._connections_by_owner.get(lease.owner_id)
            if not connections:
                return
            remaining = [item for item in connections if item.connection_id != lease.connection_id]
            if len(remaining) == len(connections):
                return
            affected_owner = lease.owner_id
            if remaining:
                self._connections_by_owner[lease.owner_id] = remaining
            else:
                self._connections_by_owner.pop(lease.owner_id, None)
            self._remove_subscription_for_connection_locked(lease.connection_id)
        if affected_owner is not None:
            self._publish_connection_counts({affected_owner})

    def count_owner_connections(self, owner_id: str) -> int:
        with self._lock:
            return self._owner_connection_count_locked(owner_id)

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
