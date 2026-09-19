// Service Worker allein für Benachrichtigungen. Chrome auf Android weigert sich,
// eine Benachrichtigung über `new Notification()` anzuzeigen — dort geht es nur über
// registration.showNotification(), und die braucht diese Datei.
//
// Bewusst ohne fetch-Handler: dieser Worker cacht nichts. Ein Cache würde die WebUI
// nach einem Update des Routers einfrieren, und gewonnen wäre damit nichts.

self.addEventListener("install", () => {
  // Sofort übernehmen, statt auf das Schließen aller Tabs zu warten.
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

// Ein Tipp auf die Benachrichtigung führt zur wartenden Session: ein offenes Fenster
// wird nach vorn geholt und umgelenkt, sonst wird eines geöffnet.
self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const ziel = event.notification.data && event.notification.data.url;

  event.waitUntil(
    (async () => {
      const fenster = await self.clients.matchAll({
        type: "window",
        includeUncontrolled: true,
      });
      for (const client of fenster) {
        if (new URL(client.url).origin !== self.location.origin) continue;
        await client.focus();
        if (ziel && "navigate" in client) {
          await client.navigate(ziel).catch(() => undefined);
        }
        return;
      }
      if (ziel) await self.clients.openWindow(ziel);
    })(),
  );
});
