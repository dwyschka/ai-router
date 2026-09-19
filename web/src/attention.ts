// Rückfragen im Browser melden.
//
// Zwei Hürden stehen dem im Weg, und beide kommen vom Browser, nicht vom Router:
//
//  1. Chrome auf Android zeigt keine Benachrichtigung über `new Notification()`,
//     sondern verlangt einen Service Worker. Deshalb wird einer registriert und,
//     wenn vorhanden, bevorzugt benutzt.
//  2. Benachrichtigungen setzen einen sicheren Kontext voraus. Über
//     http://router.homelab:7777 gibt es sie schlicht nicht — dort hilft nur HTTPS
//     oder der Webhook des Routers.
//
// Deshalb meldet erlaubnis() auch, *warum* nichts geht: die Oberfläche kann es
// dann erklären, statt den Nutzer im Dunkeln zu lassen.

/** Zustand der Erlaubnis für Systembenachrichtigungen. */
export type Erlaubnis =
  /** Der Browser kennt die Schnittstelle nicht. */
  | "nicht-verfügbar"
  /** Kein sicherer Kontext — über http:// im LAN gibt es keine Benachrichtigungen. */
  | "unsicherer-kontext"
  /** Noch nicht gefragt. */
  | "offen"
  | "erteilt"
  | "verweigert";

let registrierung: ServiceWorkerRegistration | null = null;

export function erlaubnis(): Erlaubnis {
  if (typeof Notification === "undefined") return "nicht-verfügbar";
  // Reihenfolge zählt: eine bereits erteilte Erlaubnis gilt, auch wenn die Seite
  // gerade nicht als sicher eingestuft wird.
  switch (Notification.permission) {
    case "granted":
      return "erteilt";
    case "denied":
      return "verweigert";
  }
  return window.isSecureContext ? "offen" : "unsicherer-kontext";
}

/**
 * bereiteVor registriert den Service Worker. Ohne ihn zeigt Chrome auf Android gar
 * nichts an. Scheitert die Registrierung, bleibt der direkte Weg als Rückfall.
 */
export async function bereiteVor(): Promise<void> {
  if (!("serviceWorker" in navigator) || !window.isSecureContext) return;
  try {
    registrierung = await navigator.serviceWorker.register("/sw.js");
  } catch {
    // Kein Grund aufzugeben: `new Notification()` funktioniert auf dem Desktop auch so.
  }
}

/** frageErlaubnis öffnet die Nachfrage des Browsers. */
export async function frageErlaubnis(): Promise<Erlaubnis> {
  if (typeof Notification === "undefined") return "nicht-verfügbar";
  try {
    await Notification.requestPermission();
  } catch {
    // Ältere Safari-Versionen kennen nur die Callback-Form; dann bleibt es beim
    // bisherigen Zustand.
  }
  if (erlaubnis() === "erteilt") await bereiteVor();
  return erlaubnis();
}

/**
 * melde macht auf eine Rückfrage aufmerksam: eine Systembenachrichtigung, die auf die
 * wartende Session verlinkt, dazu ein kurzes Vibrieren — das geht auch ohne Erlaubnis.
 */
export function melde(titel: string, text: string, sessionId: string): void {
  if (erlaubnis() === "erteilt") {
    const optionen: NotificationOptions = {
      body: text,
      // tag ersetzt eine ältere Meldung derselben Session, statt zu stapeln.
      tag: sessionId,
      data: { url: `${location.origin}/?session=${encodeURIComponent(sessionId)}` },
      requireInteraction: true,
      ...({ renotify: true } as object),
    };

    if (registrierung) {
      // Der Weg über den Service Worker ist der einzige, den Chrome auf Android
      // akzeptiert — und der einzige, bei dem ein Tipp die Session öffnet.
      void registrierung.showNotification(titel, optionen).catch(() => direkt(titel, optionen));
    } else {
      direkt(titel, optionen);
    }
  }
  try {
    navigator.vibrate?.([120, 60, 120]);
  } catch {
    // Vibration ist überall optional.
  }
}

// direkt ist der Weg ohne Service Worker. Auf dem Desktop reicht er.
function direkt(titel: string, optionen: NotificationOptions): void {
  try {
    new Notification(titel, optionen);
  } catch {
    // Android wirft hier; dann bleibt es beim Banner in der Oberfläche.
  }
}

/**
 * setzeTitelmarke stellt der Seitenüberschrift ein Zeichen voran, solange etwas
 * offen ist — im Tab-Wechsler sieht man das auch ohne Benachrichtigung.
 */
export function setzeTitelmarke(basis: string, offen: number): void {
  document.title = offen > 0 ? `(${offen}) ● ${basis}` : basis;
}
