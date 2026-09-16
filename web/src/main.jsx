import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";

const DEFAULT_SYMBOL = "BTC-USD";

function field(value, ...keys) {
  if (!value) return undefined;
  for (const key of keys) {
    if (value[key] !== undefined && value[key] !== null) return value[key];
  }
  return undefined;
}

function formatNumber(value) {
  if (value === undefined || value === null || value === "") return "-";
  const numeric = Number(value);
  if (!Number.isFinite(numeric)) return String(value);
  return new Intl.NumberFormat("en-US").format(numeric);
}

function statusLabel(status) {
  if (status === "live") return "Live";
  if (status === "connecting") return "Connecting";
  return "Offline";
}

function App() {
  const [symbolInput, setSymbolInput] = useState(DEFAULT_SYMBOL);
  const [symbol, setSymbol] = useState(DEFAULT_SYMBOL);
  const [status, setStatus] = useState("connecting");
  const [book, setBook] = useState({ symbol: DEFAULT_SYMBOL });
  const [trades, setTrades] = useState([]);
  const [events, setEvents] = useState([]);
  const [error, setError] = useState("");
  const socketRef = useRef(null);

  const refresh = useCallback(async (activeSymbol) => {
    const encoded = encodeURIComponent(activeSymbol);
    const [bookResponse, tradesResponse] = await Promise.all([
      fetch(`/orderbook/${encoded}`),
      fetch(`/trades/${encoded}`),
    ]);

    if (!bookResponse.ok) throw new Error(`book ${bookResponse.status}`);
    if (!tradesResponse.ok) throw new Error(`trades ${tradesResponse.status}`);

    setBook(await bookResponse.json());
    setTrades(await tradesResponse.json());
    setError("");
  }, []);

  useEffect(() => {
    let active = true;

    setStatus("connecting");
    refresh(symbol).catch((err) => {
      if (active) setError(err.message);
    });

    if (socketRef.current) socketRef.current.close();

    const protocol = location.protocol === "https:" ? "wss" : "ws";
    const socket = new WebSocket(`${protocol}://${location.host}/ws/orderbook/${encodeURIComponent(symbol)}`);
    socketRef.current = socket;

    socket.onopen = () => active && setStatus("live");
    socket.onerror = () => active && setError("websocket unavailable");
    socket.onclose = () => active && setStatus("offline");
    socket.onmessage = (event) => {
      const message = JSON.parse(event.data);
      if (message.type === "snapshot" && message.book) setBook(message.book);
      if (message.type === "events") {
        setEvents((current) => [...message.events, ...current].slice(0, 100));
        refresh(symbol).catch((err) => active && setError(err.message));
      }
    };

    return () => {
      active = false;
      socket.close();
    };
  }, [refresh, symbol]);

  const loadSymbol = useCallback(() => {
    const next = symbolInput.trim().toUpperCase();
    if (next) {
      setSymbolInput(next);
      setSymbol(next);
    }
  }, [symbolInput]);

  const bid = field(book, "best_bid", "BestBid");
  const ask = field(book, "best_ask", "BestAsk");
  const bestBid = field(bid, "price", "Price");
  const bestAsk = field(ask, "price", "Price");
  const spread = bestBid !== undefined && bestAsk !== undefined ? Number(bestAsk) - Number(bestBid) : null;
  const lastEvent = events[0];
  const lastSequence = field(lastEvent, "sequence", "Sequence");

  const totals = useMemo(() => {
    const volume = trades.reduce((sum, trade) => sum + Number(field(trade, "quantity", "Quantity") || 0), 0);
    return { trades: trades.length, volume };
  }, [trades]);

  return (
    <div className="shell">
      <header className="topbar">
        <div>
          <p className="eyebrow">Trading Engine</p>
          <h1>{symbol}</h1>
        </div>
        <div className="toolbar">
          <div className="symbol-control">
            <label htmlFor="symbol">Symbol</label>
            <input
              id="symbol"
              value={symbolInput}
              autoComplete="off"
              spellCheck="false"
              onChange={(event) => setSymbolInput(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") loadSymbol();
              }}
            />
          </div>
          <button type="button" onClick={loadSymbol}>Load</button>
          <span className={`connection ${status}`}>{statusLabel(status)}</span>
        </div>
      </header>

      {error ? <div className="notice">{error}</div> : null}

      <main className="grid">
        <section className="panel book-panel">
          <PanelHeader title="Order Book" meta={`sequence ${formatNumber(lastSequence)}`} />
          <div className="metrics">
            <Metric label="Best Bid" value={formatNumber(bestBid)} tone="buy" />
            <Metric label="Best Ask" value={formatNumber(bestAsk)} tone="sell" />
            <Metric label="Spread" value={formatNumber(spread)} />
          </div>
          <BookTable bid={bid} ask={ask} />
        </section>

        <section className="panel">
          <PanelHeader title="Tape" meta={`${formatNumber(totals.trades)} trades`} />
          <div className="metrics compact">
            <Metric label="Volume" value={formatNumber(totals.volume)} />
            <Metric label="Events" value={formatNumber(events.length)} />
          </div>
          <TradesTable trades={trades} />
        </section>

        <section className="panel events-panel">
          <PanelHeader title="Event Stream" meta={statusLabel(status)} />
          <EventLog events={events} />
        </section>
      </main>
    </div>
  );
}

function PanelHeader({ title, meta }) {
  return (
    <div className="panel-head">
      <h2>{title}</h2>
      <span>{meta}</span>
    </div>
  );
}

function Metric({ label, value, tone }) {
  return (
    <div className="metric">
      <span>{label}</span>
      <strong className={tone || ""}>{value}</strong>
    </div>
  );
}

function BookTable({ bid, ask }) {
  const rows = [
    ["Bid", bid, "buy"],
    ["Ask", ask, "sell"],
  ];

  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Side</th>
            <th>Price</th>
            <th>Quantity</th>
            <th>Orders</th>
          </tr>
        </thead>
        <tbody>
          {rows.map(([label, level, tone]) => (
            <tr key={label}>
              <td className={tone}>{label}</td>
              <td>{formatNumber(field(level, "price", "Price"))}</td>
              <td>{formatNumber(field(level, "quantity", "Quantity"))}</td>
              <td>{formatNumber(field(level, "orders", "Orders"))}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function TradesTable({ trades }) {
  const rows = trades.slice(-14).reverse();

  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Sequence</th>
            <th>Maker</th>
            <th>Taker</th>
            <th>Price</th>
            <th>Quantity</th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td className="empty" colSpan="5">No trades yet</td>
            </tr>
          ) : rows.map((trade, index) => (
            <tr key={`${field(trade, "sequence", "Sequence")}-${index}`}>
              <td>{formatNumber(field(trade, "sequence", "Sequence"))}</td>
              <td>{field(trade, "maker_order_id", "MakerOrderID") || "-"}</td>
              <td>{field(trade, "taker_order_id", "TakerOrderID") || "-"}</td>
              <td>{formatNumber(field(trade, "price", "Price"))}</td>
              <td>{formatNumber(field(trade, "quantity", "Quantity"))}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function EventLog({ events }) {
  if (events.length === 0) {
    return <div className="empty-log">No events yet</div>;
  }

  return (
    <div className="event-log">
      {events.map((event, index) => (
        <div className="event-row" key={`${field(event, "sequence", "Sequence")}-${field(event, "type", "Type")}-${index}`}>
          <span>{formatNumber(field(event, "sequence", "Sequence"))}</span>
          <strong>{field(event, "type", "Type")}</strong>
        </div>
      ))}
    </div>
  );
}

createRoot(document.getElementById("root")).render(<App />);
