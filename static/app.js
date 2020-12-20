const Controller = {
  search: (ev) => {
    ev.preventDefault();
    const form = document.getElementById("form");
    const data = Object.fromEntries(new FormData(form));
    const response = fetch(`/search?q=${data.query}`).then((response) => {
      response.json().then((results) => {
        Controller.updateTable(results.results);
        Controller.showTimeTaken(results.time, results.results.length);
        Controller.showSearchCorrection(results.replaced);
      });
    });
  },

  updateTable: (results) => {
    const table = document.getElementById("results");
    const rows = [];
    for (let result of results) {
      rows.push(`<li>${result}</li>`);
    }
    table.innerHTML = rows.join("");
  },

  showTimeTaken: (timeTaken, numResults) => {
    const span = document.getElementById("time-taken");
    span.innerHTML = "Found " + numResults + " results in "+timeTaken;
  },

  showSearchCorrection: (replaced) => {
    const span = document.getElementById("search-correction");
    var query = document.getElementById("query").value
    if (replaced.length > 0) {
      for (i = 0; i < replaced.length; i+=2) {
        query = query.replaceAll(replaced[i], "<em>"+replaced[i+1]+"</em>")
      }
      span.innerHTML = "Searching for "+query;
    } else {
      span.innerHTML = "";
    }
  },
};

const form = document.getElementById("form");
form.addEventListener("submit", Controller.search);
