import React, { useCallback, useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";

const DEFAULT_SYMBOL = "BTC-USD";

function format(value) {
  return value === undefined || value === null ? "-" : String(value);
}

function App() {
  const [symbolInput, setSymbolInput] = useState(DEFAULT_SYMBOL);
  const [symbol, setSymbol] = useState(DEFAULT_SYMBOL);
  const [status, setStatus] = useState("offline");
  const [book, setBook] = useState({ symbol: DEFAULT_SYMBOL });
  const [trades, setTrades] = useState([]);
  const [events, setEvents] = useState([]);
  const socketRef = useRef(null);

  const refresh = useCallback(async (activeSymbol) => {
    const encoded = encodeURIComponent(activeSymbol);
    const [bookResponse, tradesResponse] = await Promise.all([
      fetch(`/orderbook/${encoded}`),
      fetch(`/trades/${encoded}`),
    ]);

    if (bookResponse.ok) setBook(await bookResponse.json());
    if (tradesResponse.ok) setTrades(await tradesResponse.json());
  }, []);

  useEffect(() => {
    refresh(symbol);
    if (socketRef.current) socketRef.current.close();

    const protocol = location.protocol === "https:" ? "wss" : "ws";
    const socket = new WebSocket(`${protocol}://${location.host}/ws/orderbook/${encodeURIComponent(symbol)}`);
    socketRef.current = socket;

    socket.onopen = () => setStatus("live");
    socket.onclose = () => setStatus("offline");
    socket.onmessage = (event) => {
      const message = JSON.parse(event.data);
      if (message.type === "snapshot" && message.book) setBook(message.book);
      if (message.type === "events") {
        setEvents((current) => message.events.concat(current).slice(0, 80));
        refresh(symbol);
      }
    };

    return () => socket.close();
  }, [refresh, symbol]);

  const loadSymbol = () => {
    const next = symbolInput.trim();
    if (next) setSymbol(next);
  };

  const bid = book.best_bid;
  const ask = book.best_ask;
  const spread = bid && ask ? ask.Price - bid.Price : null;

  return (
    <>
      <header>
        <h1>Trading Engine</h1>
        <div className="toolbar">
          <label htmlFor="symbol">Symbol</label>
          <input
            id="symbol"
            value={symbolInput}
            autoComplete="off"
            onChange={(event) => setSymbolInput(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") loadSymbol();
            }}
          />
          <button type="button" onClick={loadSymbol}>Load</button>
          <div className="status">{status}</div>
        </div>
      </header>
      <main>
        <section>
          <div className="section-head">
            <h2>Book</h2>
            <span className="status">sequence {events[0]?.Sequence || "-"}</span>
          </div>
          <div className="metric-row">
            <Metric label="Best Bid" value={bid?.Price} />
            <Metric label="Best Ask" value={ask?.Price} />
            <Metric label="Spread" value={spread} />
          </div>
          <BookTable bid={bid} ask={ask} />
        </section>
        <section>
          <div className="section-head">
            <h2>Trades</h2>
            <span className="status">{trades.length} trades</span>
          </div>
          <TradesTable trades={trades} />
          <div className="section-head"><h2>Events</h2></div>
          <EventLog events={events} />
        </section>
      </main>
    </>
  );
}

function Metric({ label, value }) {
  return (
    <div className="metric">
      <span>{label}</span>
      <strong>{format(value)}</strong>
    </div>
  );
}

function BookTable({ bid, ask }) {
  const rows = [
    ["Bid", bid, "buy"],
    ["Ask", ask, "sell"],
  ];

  return (
    <table>
      <thead>
        <tr><th>Side</th><th>Price</th><th>Quantity</th><th>Orders</th></tr>
      </thead>
      <tbody>
        {rows.map(([label, level, className]) => (
          <tr key={label}>
            <td className={className}>{label}</td>
            <td>{format(level?.Price)}</td>
            <td>{format(level?.Quantity)}</td>
            <td>{format(level?.Orders)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function TradesTable({ trades }) {
  return (
    <table>
      <thead>
        <tr><th>Sequence</th><th>Maker</th><th>Taker</th><th>Price</th><th>Quantity</th></tr>
      </thead>
      <tbody>
        {trades.slice(-12).reverse().map((trade, index) => (
          <tr key={`${trade.Sequence}-${index}`}>
            <td>{trade.Sequence}</td>
            <td>{trade.MakerOrderID}</td>
            <td>{trade.TakerOrderID}</td>
            <td>{trade.Price}</td>
            <td>{trade.Quantity}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function EventLog({ events }) {
  return (
    <div className="event-log">
      {events.map((event, index) => (
        <div key={`${event.Sequence}-${event.Type}-${index}`}>{event.Sequence} {event.Type}</div>
      ))}
    </div>
  );
}

createRoot(document.getElementById("root")).render(<App />);
